package signaling

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/msk1039/termcall/internal/protocol"
	"github.com/pion/logging"
	pionturn "github.com/pion/turn/v4"
	"golang.org/x/time/rate"
)

type testCredentialIssuer struct{}

func (testCredentialIssuer) Issue(_ string) (protocol.ICEServer, error) {
	return protocol.ICEServer{
		URLs:       []string{"stun:127.0.0.1:3478", "turn:127.0.0.1:3478"},
		Username:   "future:test-peer",
		Credential: "ephemeral-credential",
		ExpiresAt:  time.Now().Add(10 * time.Minute).Unix(),
	}, nil
}

func TestSignalingAdmissionRoutingLeaveAndCapacity(t *testing.T) {
	serverURL, closeServer := startTestServer(t, ServerConfig{
		CredentialIssuer:    testCredentialIssuer{},
		MaxRoomSize:         2,
		MaxRooms:            10,
		MaxConnections:      100,
		MaxConnectionsPerIP: 10,
		MessageRate:         rate.Limit(100),
		MessageBurst:        100,
	})
	defer closeServer()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, firstAdmission, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   "new",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer first.Close()
	if firstAdmission.RoomID == "new" || firstAdmission.PeerID == "" {
		t.Fatalf("invalid first admission: %#v", firstAdmission)
	}

	second, secondAdmission, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   firstAdmission.RoomID,
		Username: "bob",
	})
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	defer second.Close()
	if len(secondAdmission.Peers) != 1 || secondAdmission.Peers[0].PeerID != firstAdmission.PeerID {
		t.Fatalf("second peer snapshot = %#v", secondAdmission.Peers)
	}

	joined := receiveMessage(t, first.Receive())
	if joined.Type != protocol.MsgPeerJoined || joined.PeerID != secondAdmission.PeerID || joined.Username != "bob" {
		t.Fatalf("peer joined message = %#v", joined)
	}

	sdp, err := protocol.MarshalPayload(protocol.SDPPayload{Type: "offer", SDP: "v=0\r\n"})
	if err != nil {
		t.Fatalf("marshal SDP: %v", err)
	}
	offer := protocol.NewMessage(protocol.MsgOffer)
	offer.PeerID = secondAdmission.PeerID
	offer.Payload = sdp
	if err := first.Send(offer); err != nil {
		t.Fatalf("send offer: %v", err)
	}
	routed := receiveMessage(t, second.Receive())
	if routed.Type != protocol.MsgOffer || routed.PeerID != firstAdmission.PeerID {
		t.Fatalf("routed offer = %#v", routed)
	}

	third, _, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   firstAdmission.RoomID,
		Username: "charlie",
	})
	if err == nil {
		third.Close()
		t.Fatal("third peer unexpectedly joined a full room")
	}
	if !strings.Contains(err.Error(), "room_full") {
		t.Fatalf("third dial error = %v", err)
	}

	if err := second.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Logf("second close: %v", err)
	}
	left := receiveMessage(t, first.Receive())
	if left.Type != protocol.MsgPeerLeft || left.PeerID != secondAdmission.PeerID {
		t.Fatalf("peer left message = %#v", left)
	}

	replacement, replacementAdmission, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   firstAdmission.RoomID,
		Username: "charlie",
	})
	if err != nil {
		t.Fatalf("replacement dial: %v", err)
	}
	defer replacement.Close()
	if len(replacementAdmission.Peers) != 1 || replacementAdmission.Peers[0].PeerID != firstAdmission.PeerID {
		t.Fatalf("replacement snapshot = %#v", replacementAdmission.Peers)
	}
}

func TestSignalingFlushesFatalProtocolError(t *testing.T) {
	serverURL, closeServer := startTestServer(t, defaultTestServerConfig())
	defer closeServer()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, serverURL, nil)
	if err != nil {
		t.Fatalf("dial raw websocket: %v", err)
	}
	defer connection.CloseNow()

	invalid := protocol.NewMessage(protocol.MsgJoinRoom)
	invalid.Version = protocol.Version + 1
	invalid.RoomID = "new"
	invalid.Username = "alice"
	if err := wsjson.Write(ctx, connection, invalid); err != nil {
		t.Fatalf("write invalid message: %v", err)
	}
	var response protocol.SignalingMessage
	if err := wsjson.Read(ctx, connection, &response); err != nil {
		t.Fatalf("read fatal response: %v", err)
	}
	failure, err := protocol.DecodePayload[protocol.ErrorPayload](response.Payload)
	if err != nil {
		t.Fatalf("decode fatal response: %v", err)
	}
	if response.Type != protocol.MsgError || failure.Code != "unsupported_version" {
		t.Fatalf("fatal response = %#v, payload = %#v", response, failure)
	}
}

func TestSignalingDoesNotCreateUserSelectedRoom(t *testing.T) {
	serverURL, closeServer := startTestServer(t, defaultTestServerConfig())
	defer closeServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   "2d54fa38-03a6-44af-9db6-0fd672d42f95",
		Username: "alice",
	})
	if client != nil {
		client.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "room_not_found") {
		t.Fatalf("unknown-room error = %v", err)
	}
}

func TestSignalingConnectionAndOriginLimits(t *testing.T) {
	config := defaultTestServerConfig()
	config.MaxConnectionsPerIP = 1
	config.MaxConnections = 1
	config.MaxRooms = 1
	config.AllowedOrigins = []string{"trusted.example"}
	serverURL, closeServer := startTestServer(t, config)
	defer closeServer()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	originHeader := http.Header{"Origin": []string{"https://evil.example"}}
	rejected, response, err := websocket.Dial(ctx, serverURL, &websocket.DialOptions{HTTPHeader: originHeader})
	if rejected != nil {
		rejected.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("origin rejection error = %v, status = %d", err, status)
	}

	first, _, err := websocket.Dial(ctx, serverURL, nil)
	if err != nil {
		t.Fatalf("first connection: %v", err)
	}
	defer first.CloseNow()

	secondHeader := http.Header{"X-Forwarded-For": []string{"198.51.100.8"}}
	second, response, err := websocket.Dial(ctx, serverURL, &websocket.DialOptions{HTTPHeader: secondHeader})
	if second != nil {
		second.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		t.Fatalf("second connection error = %v, status = %d", err, status)
	}
}

func TestSignalingRateLimitReturnsTypedError(t *testing.T) {
	config := defaultTestServerConfig()
	config.MessageRate = rate.Limit(0.001)
	config.MessageBurst = 1
	serverURL, closeServer := startTestServer(t, config)
	defer closeServer()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := Dial(ctx, DialOptions{URL: serverURL, RoomID: "new", Username: "alice"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	if err := client.Send(protocol.NewMessage(protocol.MsgLeaveRoom)); err != nil {
		t.Fatalf("send rate-limited message: %v", err)
	}
	message := receiveMessage(t, client.Receive())
	if message.Type != protocol.MsgError {
		t.Fatalf("rate-limit response = %#v", message)
	}
	failure, err := protocol.DecodePayload[protocol.ErrorPayload](message.Payload)
	if err != nil || failure.Code != "rate_limited" {
		t.Fatalf("rate-limit payload = %#v, error = %v", failure, err)
	}
}

func TestLiveSignalingAdmission(t *testing.T) {
	serverURL := os.Getenv("TERMCALL_LIVE_URL")
	if serverURL == "" {
		t.Skip("TERMCALL_LIVE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first, admission, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   "new",
		Username: "deployment-smoke-one",
	})
	if err != nil {
		t.Fatalf("live admission: %v", err)
	}
	defer first.Close()
	if admission.RoomID == "new" || admission.PeerID == "" {
		t.Fatalf("invalid live admission: %#v", admission)
	}
	second, secondAdmission, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   admission.RoomID,
		Username: "deployment-smoke-two",
	})
	if err != nil {
		t.Fatalf("second live admission: %v", err)
	}
	if len(secondAdmission.Peers) != 1 || secondAdmission.Peers[0].PeerID != admission.PeerID {
		second.Close()
		t.Fatalf("second live peer snapshot = %#v", secondAdmission.Peers)
	}
	joined := receiveMessage(t, first.Receive())
	if joined.Type != protocol.MsgPeerJoined || joined.PeerID != secondAdmission.PeerID {
		second.Close()
		t.Fatalf("live peer-joined message = %#v", joined)
	}
	if err := second.Close(); err != nil {
		t.Logf("close second live client: %v", err)
	}
	left := receiveMessage(t, first.Receive())
	if left.Type != protocol.MsgPeerLeft || left.PeerID != secondAdmission.PeerID {
		t.Fatalf("live peer-left message = %#v", left)
	}
}

func TestLiveTURNAllocation(t *testing.T) {
	serverURL := os.Getenv("TERMCALL_LIVE_URL")
	turnAddress := os.Getenv("TERMCALL_LIVE_TURN_ADDR")
	if serverURL == "" || turnAddress == "" {
		t.Skip("TERMCALL_LIVE_URL and TERMCALL_LIVE_TURN_ADDR are not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, admission, err := Dial(ctx, DialOptions{
		URL:      serverURL,
		RoomID:   "new",
		Username: "turn-allocation-smoke",
	})
	if err != nil {
		t.Fatalf("live admission: %v", err)
	}
	defer client.Close()
	if len(admission.ICEServers) == 0 {
		t.Fatal("live admission returned no TURN credential")
	}
	credential := admission.ICEServers[0]

	packetConnection, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("open TURN client socket: %v", err)
	}
	defer packetConnection.Close()
	turnClient, err := pionturn.NewClient(&pionturn.ClientConfig{
		STUNServerAddr: turnAddress,
		TURNServerAddr: turnAddress,
		Username:       credential.Username,
		Password:       credential.Credential,
		Realm:          "termcall",
		RTO:            500 * time.Millisecond,
		Conn:           packetConnection,
		LoggerFactory:  logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		t.Fatalf("create TURN client: %v", err)
	}
	defer turnClient.Close()
	if err := turnClient.Listen(); err != nil {
		t.Fatalf("listen for TURN responses: %v", err)
	}
	relayConnection, err := turnClient.Allocate()
	if err != nil {
		t.Fatalf("allocate TURN relay: %v", err)
	}
	defer relayConnection.Close()
	if relayConnection.LocalAddr() == nil {
		t.Fatal("TURN allocation returned no relay address")
	}
}

func defaultTestServerConfig() ServerConfig {
	return ServerConfig{
		CredentialIssuer:    testCredentialIssuer{},
		MaxRoomSize:         5,
		MaxRooms:            10,
		MaxConnections:      100,
		MaxConnectionsPerIP: 10,
		MessageRate:         rate.Limit(100),
		MessageBurst:        100,
	}
}

func startTestServer(t *testing.T, config ServerConfig) (string, func()) {
	t.Helper()
	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("new signaling server: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", server.HandleWebSocket)
	httpServer := httptest.NewServer(mux)
	serverURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	return serverURL, func() {
		server.Close()
		httpServer.Close()
	}
}

func receiveMessage(t *testing.T, messages <-chan protocol.SignalingMessage) protocol.SignalingMessage {
	t.Helper()
	select {
	case message, ok := <-messages:
		if !ok {
			t.Fatal("signaling message channel closed")
		}
		return message
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for signaling message")
		return protocol.SignalingMessage{}
	}
}
