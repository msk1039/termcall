package signaling

import (
	"strings"
	"testing"
	"time"

	"github.com/msk1039/termcall/internal/protocol"
)

func TestValidateServerURL(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		allowInsecure bool
		wantErr       bool
	}{
		{name: "secure", url: "wss://call.example/ws"},
		{name: "loopback hostname", url: "ws://localhost:8080/ws"},
		{name: "loopback IP", url: "ws://127.0.0.1:8080/ws"},
		{name: "explicit development opt-in", url: "ws://192.0.2.1:8080/ws", allowInsecure: true},
		{name: "public plaintext", url: "ws://call.example/ws", wantErr: true},
		{name: "HTTP", url: "https://call.example/ws", wantErr: true},
		{name: "user info", url: "wss://user:pass@call.example/ws", wantErr: true},
		{name: "missing host", url: "wss:///ws", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateServerURL(test.url, test.allowInsecure)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateAdmissionRejectsConfusingResponses(t *testing.T) {
	valid := protocol.JoinAcceptedPayload{
		RoomID: "2d54fa38-03a6-44af-9db6-0fd672d42f95",
		PeerID: "local-peer",
		Peers:  []protocol.PeerInfo{{PeerID: "remote-peer", Username: "alice"}},
		ICEServers: []protocol.ICEServer{{
			URLs:       []string{"turn:1.1.1.1:3478"},
			Username:   "future:local-peer",
			Credential: "credential",
			ExpiresAt:  time.Now().Add(10 * time.Minute).Unix(),
		}},
	}
	if err := validateAdmission(valid); err != nil {
		t.Fatalf("valid admission: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*protocol.JoinAcceptedPayload)
	}{
		{name: "invalid room", mutate: func(value *protocol.JoinAcceptedPayload) { value.RoomID = "bad/room" }},
		{name: "invalid local peer", mutate: func(value *protocol.JoinAcceptedPayload) { value.PeerID = "bad/peer" }},
		{name: "no ICE", mutate: func(value *protocol.JoinAcceptedPayload) { value.ICEServers = nil }},
		{name: "expired ICE", mutate: func(value *protocol.JoinAcceptedPayload) {
			value.ICEServers[0].ExpiresAt = time.Now().Add(-time.Minute).Unix()
		}},
		{name: "unsafe ICE URL", mutate: func(value *protocol.JoinAcceptedPayload) { value.ICEServers[0].URLs = []string{"https://example.com"} }},
		{name: "missing credential", mutate: func(value *protocol.JoinAcceptedPayload) { value.ICEServers[0].Credential = "" }},
		{name: "local in snapshot", mutate: func(value *protocol.JoinAcceptedPayload) { value.Peers[0].PeerID = value.PeerID }},
		{name: "duplicate peer", mutate: func(value *protocol.JoinAcceptedPayload) { value.Peers = append(value.Peers, value.Peers[0]) }},
		{name: "invalid peer username", mutate: func(value *protocol.JoinAcceptedPayload) { value.Peers[0].Username = "\n" }},
		{name: "too many peers", mutate: func(value *protocol.JoinAcceptedPayload) { value.Peers = make([]protocol.PeerInfo, 17) }},
		{name: "too many ICE servers", mutate: func(value *protocol.JoinAcceptedPayload) { value.ICEServers = make([]protocol.ICEServer, 5) }},
		{name: "long credential", mutate: func(value *protocol.JoinAcceptedPayload) { value.ICEServers[0].Credential = strings.Repeat("x", 513) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			value.Peers = append([]protocol.PeerInfo{}, valid.Peers...)
			value.ICEServers = append([]protocol.ICEServer{}, valid.ICEServers...)
			value.ICEServers[0].URLs = append([]string{}, valid.ICEServers[0].URLs...)
			test.mutate(&value)
			if err := validateAdmission(value); err == nil {
				t.Fatal("invalid admission unexpectedly succeeded")
			}
		})
	}
}
