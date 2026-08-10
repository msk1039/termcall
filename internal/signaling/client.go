package signaling

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/msk1039/termcall/internal/protocol"
)

const joinTimeout = 10 * time.Second

type DialOptions struct {
	URL             string
	RoomID          string
	Username        string
	AllowInsecureWS bool
}

type Client struct {
	conn   *websocket.Conn
	recv   chan protocol.SignalingMessage
	errors chan error
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	close  sync.Once
}

func Dial(ctx context.Context, options DialOptions) (*Client, protocol.JoinAcceptedPayload, error) {
	var emptyAdmission protocol.JoinAcceptedPayload
	if err := validateServerURL(options.URL, options.AllowInsecureWS); err != nil {
		return nil, emptyAdmission, err
	}
	if err := protocol.ValidateRoomID(options.RoomID); err != nil {
		return nil, emptyAdmission, err
	}
	username, err := protocol.NormalizeUsername(options.Username)
	if err != nil {
		return nil, emptyAdmission, err
	}

	clientCtx, cancel := context.WithCancel(ctx)
	connection, _, err := websocket.Dial(clientCtx, options.URL, nil)
	if err != nil {
		cancel()
		return nil, emptyAdmission, fmt.Errorf("dial signaling server: %w", err)
	}
	connection.SetReadLimit(protocol.MaxSignalMessageBytes)

	client := &Client{
		conn:   connection,
		recv:   make(chan protocol.SignalingMessage, peerQueueSize),
		errors: make(chan error, 1),
		ctx:    clientCtx,
		cancel: cancel,
	}

	join := protocol.NewMessage(protocol.MsgJoinRoom)
	join.RoomID = options.RoomID
	join.Username = username
	joinCtx, joinCancel := context.WithTimeout(clientCtx, joinTimeout)
	defer joinCancel()
	if err := wsjson.Write(joinCtx, connection, join); err != nil {
		client.abort()
		return nil, emptyAdmission, fmt.Errorf("send join request: %w", err)
	}

	messageType, responseData, err := connection.Read(joinCtx)
	if err != nil {
		client.abort()
		return nil, emptyAdmission, fmt.Errorf("wait for join response: %w", err)
	}
	if messageType != websocket.MessageText {
		client.abort()
		return nil, emptyAdmission, errors.New("join response is not JSON text")
	}
	response, err := protocol.DecodeMessage(responseData)
	if err != nil {
		client.abort()
		return nil, emptyAdmission, fmt.Errorf("decode join response: %w", err)
	}
	if response.Version != protocol.Version {
		client.abort()
		return nil, emptyAdmission, protocol.ErrProtocolVersion
	}
	if response.Type == protocol.MsgError {
		if err := protocol.ValidateServerMessage(response); err != nil {
			client.abort()
			return nil, emptyAdmission, errors.New("server rejected join with an invalid error response")
		}
		failure, decodeErr := protocol.DecodePayload[protocol.ErrorPayload](response.Payload)
		client.abort()
		if decodeErr != nil {
			return nil, emptyAdmission, errors.New("server rejected join with an invalid error response")
		}
		return nil, emptyAdmission, fmt.Errorf("join rejected (%s): %s", failure.Code, failure.Message)
	}
	if response.Type != protocol.MsgJoinAccepted {
		client.abort()
		return nil, emptyAdmission, fmt.Errorf("unexpected join response %q", response.Type)
	}

	admission, err := protocol.DecodePayload[protocol.JoinAcceptedPayload](response.Payload)
	if err != nil {
		client.abort()
		return nil, emptyAdmission, err
	}
	if err := validateAdmission(admission); err != nil {
		client.abort()
		return nil, emptyAdmission, err
	}
	if response.RoomID != admission.RoomID || response.PeerID != admission.PeerID {
		client.abort()
		return nil, emptyAdmission, errors.New("join response envelope does not match its payload")
	}

	go client.readPump()
	return client, admission, nil
}

func (client *Client) Send(message protocol.SignalingMessage) error {
	message.Version = protocol.Version
	if err := protocol.ValidateClientMessage(message); err != nil {
		return err
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	ctx, cancel := context.WithTimeout(client.ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, client.conn, message)
}

func (client *Client) Receive() <-chan protocol.SignalingMessage {
	return client.recv
}

func (client *Client) Errors() <-chan error {
	return client.errors
}

func (client *Client) Close() error {
	var closeErr error
	client.close.Do(func() {
		client.mu.Lock()
		leave := protocol.NewMessage(protocol.MsgLeaveRoom)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = wsjson.Write(ctx, client.conn, leave)
		cancel()
		closeErr = client.conn.Close(websocket.StatusNormalClosure, "")
		client.mu.Unlock()
		client.cancel()
	})
	return closeErr
}

func (client *Client) abort() {
	client.cancel()
	_ = client.conn.Close(websocket.StatusInternalError, "join failed")
}

func (client *Client) readPump() {
	defer client.cancel()
	defer client.conn.CloseNow()
	defer close(client.recv)
	defer close(client.errors)
	for {
		messageType, data, err := client.conn.Read(client.ctx)
		if err != nil {
			if !isDisconnect(err) {
				client.reportError(fmt.Errorf("read signaling message: %w", err))
			}
			return
		}
		if messageType != websocket.MessageText {
			client.reportError(errors.New("signaling server sent a non-text message"))
			return
		}
		message, err := protocol.DecodeMessage(data)
		if err != nil {
			client.reportError(fmt.Errorf("decode signaling message: %w", err))
			return
		}
		if err := protocol.ValidateServerMessage(message); err != nil {
			client.reportError(fmt.Errorf("invalid signaling message: %w", err))
			return
		}

		select {
		case client.recv <- message:
		case <-client.ctx.Done():
			return
		default:
			client.reportError(errors.New("local signaling consumer is too slow"))
			client.cancel()
			return
		}
	}
}

func (client *Client) reportError(err error) {
	select {
	case client.errors <- err:
	default:
	}
}

func validateAdmission(admission protocol.JoinAcceptedPayload) error {
	if err := protocol.ValidateRoomID(admission.RoomID); err != nil {
		return fmt.Errorf("invalid room in join response: %w", err)
	}
	if err := protocol.ValidatePeerID(admission.PeerID); err != nil {
		return fmt.Errorf("invalid local peer in join response: %w", err)
	}
	if len(admission.ICEServers) == 0 {
		return errors.New("join response has no ICE servers")
	}
	if len(admission.ICEServers) > 4 {
		return errors.New("join response has too many ICE servers")
	}
	if len(admission.Peers) > 16 {
		return errors.New("join response has too many peers")
	}
	now := time.Now().Unix()
	for _, server := range admission.ICEServers {
		if len(server.URLs) == 0 || len(server.URLs) > 8 || server.Username == "" || len(server.Username) > 256 || server.Credential == "" || len(server.Credential) > 512 {
			return errors.New("join response has incomplete ICE credentials")
		}
		if server.ExpiresAt <= now || server.ExpiresAt > now+int64((2*time.Hour)/time.Second) {
			return errors.New("join response has expired ICE credentials")
		}
		for _, rawURL := range server.URLs {
			isICEURL := strings.HasPrefix(rawURL, "stun:") || strings.HasPrefix(rawURL, "turn:") || strings.HasPrefix(rawURL, "turns:")
			if !isICEURL || len(rawURL) > 512 || strings.ContainsAny(rawURL, "\r\n\x00") {
				return errors.New("join response contains an invalid ICE server URL")
			}
		}
	}
	seenPeers := make(map[string]struct{}, len(admission.Peers))
	for _, peer := range admission.Peers {
		if err := protocol.ValidatePeerID(peer.PeerID); err != nil {
			return fmt.Errorf("invalid peer in join response: %w", err)
		}
		if peer.PeerID == admission.PeerID {
			return errors.New("join response includes the local peer in its peer snapshot")
		}
		if _, exists := seenPeers[peer.PeerID]; exists {
			return errors.New("join response contains a duplicate peer")
		}
		seenPeers[peer.PeerID] = struct{}{}
		if _, err := protocol.NormalizeUsername(peer.Username); err != nil {
			return fmt.Errorf("invalid username in join response: %w", err)
		}
	}
	return nil
}

func validateServerURL(rawURL string, allowInsecure bool) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse signaling URL: %w", err)
	}
	if parsed.User != nil || parsed.Hostname() == "" {
		return errors.New("signaling URL must contain a host and no user information")
	}
	switch parsed.Scheme {
	case "wss":
		return nil
	case "ws":
		if allowInsecure || isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return errors.New("insecure ws:// is allowed only for loopback addresses or with explicit opt-in")
	default:
		return errors.New("signaling URL must use wss:// or ws://")
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
