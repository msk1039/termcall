package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeUsername(t *testing.T) {
	longName := strings.Repeat("界", MaxUsernameRunes+10)
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim", input: "  Alice  ", want: "Alice"},
		{name: "remove controls", input: "A\x1b[31m\nB", want: "A[31mB"},
		{name: "unicode", input: "नमस्ते", want: "नमस्ते"},
		{name: "truncate by runes", input: longName, want: strings.Repeat("界", MaxUsernameRunes)},
		{name: "empty", input: " \n\t", wantErr: ErrInvalidUsername},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeUsername(test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("username = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIdentifierValidation(t *testing.T) {
	validRooms := []string{"new", "2d54fa38-03a6-44af-9db6-0fd672d42f95"}
	for _, roomID := range validRooms {
		if err := ValidateRoomID(roomID); err != nil {
			t.Errorf("ValidateRoomID(%q): %v", roomID, err)
		}
	}
	invalidRooms := []string{"", "room-1", "भारत.1", "room/one", "2D54FA38-03A6-44AF-9DB6-0FD672D42F95", strings.Repeat("a", MaxRoomIDBytes+1)}
	for _, roomID := range invalidRooms {
		if !errors.Is(ValidateRoomID(roomID), ErrInvalidRoomID) {
			t.Errorf("ValidateRoomID(%q) unexpectedly succeeded", roomID)
		}
	}
	if !errors.Is(ValidatePeerID(""), ErrInvalidPeerID) || !errors.Is(ValidatePeerID("peer/one"), ErrInvalidPeerID) {
		t.Fatal("invalid peer ID unexpectedly succeeded")
	}
}

func TestValidateClientMessage(t *testing.T) {
	offerPayload := mustPayload(t, SDPPayload{Type: "offer", SDP: "v=0\r\n"})
	answerPayload := mustPayload(t, SDPPayload{Type: "answer", SDP: "v=0\r\n"})
	candidatePayload := mustPayload(t, ICECandidatePayload{Candidate: "candidate:1 1 UDP 1 203.0.113.1 5000 typ host"})
	tests := []struct {
		name    string
		message SignalingMessage
		wantErr error
	}{
		{name: "join", message: SignalingMessage{Version: Version, Type: MsgJoinRoom, RoomID: "new", Username: "alice"}},
		{name: "offer", message: SignalingMessage{Version: Version, Type: MsgOffer, PeerID: "peer-1", Payload: offerPayload}},
		{name: "answer", message: SignalingMessage{Version: Version, Type: MsgAnswer, PeerID: "peer-1", Payload: answerPayload}},
		{name: "candidate", message: SignalingMessage{Version: Version, Type: MsgICECandidate, PeerID: "peer-1", Payload: candidatePayload}},
		{name: "version", message: SignalingMessage{Version: Version + 1, Type: MsgLeaveRoom}, wantErr: ErrProtocolVersion},
		{name: "join peer field", message: SignalingMessage{Version: Version, Type: MsgJoinRoom, RoomID: "new", Username: "alice", PeerID: "peer-1"}, wantErr: ErrInvalidMessage},
		{name: "leave fields", message: SignalingMessage{Version: Version, Type: MsgLeaveRoom, RoomID: "room"}, wantErr: ErrInvalidMessage},
		{name: "offer type mismatch", message: SignalingMessage{Version: Version, Type: MsgOffer, PeerID: "peer-1", Payload: answerPayload}, wantErr: ErrInvalidMessage},
		{name: "empty candidate", message: SignalingMessage{Version: Version, Type: MsgICECandidate, PeerID: "peer-1", Payload: mustPayload(t, ICECandidatePayload{})}, wantErr: ErrInvalidMessage},
		{name: "candidate control", message: SignalingMessage{Version: Version, Type: MsgICECandidate, PeerID: "peer-1", Payload: mustPayload(t, ICECandidatePayload{Candidate: "bad\nvalue"})}, wantErr: ErrInvalidMessage},
		{name: "server-only type", message: SignalingMessage{Version: Version, Type: MsgPeerJoined, PeerID: "peer-1", Username: "alice"}, wantErr: ErrInvalidMessage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateClientMessage(test.message)
			if test.wantErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestDecodePayloadIsStrictAndBounded(t *testing.T) {
	unknown := json.RawMessage(`{"type":"offer","sdp":"v=0","extra":true}`)
	if _, err := DecodePayload[SDPPayload](unknown); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("unknown field error = %v", err)
	}
	trailing := json.RawMessage(`{"type":"offer","sdp":"v=0"} {}`)
	if _, err := DecodePayload[SDPPayload](trailing); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("trailing data error = %v", err)
	}
	tooLarge := json.RawMessage(strings.Repeat("x", MaxPayloadBytes+1))
	if _, err := DecodePayload[SDPPayload](tooLarge); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("large payload error = %v", err)
	}
}

func TestDecodeMessageIsStrictAndBounded(t *testing.T) {
	valid, err := DecodeMessage([]byte(`{"version":1,"type":"leave_room"}`))
	if err != nil || valid.Type != MsgLeaveRoom {
		t.Fatalf("valid message = %#v, error = %v", valid, err)
	}
	invalid := [][]byte{
		[]byte(`{"version":1,"type":"leave_room","extra":true}`),
		[]byte(`{"version":1,"type":"leave_room"} {}`),
		[]byte{},
		[]byte(strings.Repeat("x", MaxSignalMessageBytes+1)),
	}
	for _, data := range invalid {
		if _, err := DecodeMessage(data); !errors.Is(err, ErrInvalidMessage) {
			t.Fatalf("DecodeMessage(%q) error = %v", data, err)
		}
	}
}

func TestValidateServerErrorRejectsTerminalControls(t *testing.T) {
	message := NewMessage(MsgError)
	message.Payload = mustPayload(t, ErrorPayload{Code: "bad_request", Message: "bad\x1b[31m"})
	if err := ValidateServerMessage(message); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("error = %v", err)
	}
}

func FuzzValidateClientMessage(f *testing.F) {
	f.Add([]byte(`{"version":1,"type":"join_room","room_id":"new","username":"alice"}`))
	f.Add([]byte(`{"version":99,"type":"leave_room"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxSignalMessageBytes*2 {
			t.Skip()
		}
		message, err := DecodeMessage(data)
		if err != nil {
			return
		}
		_ = ValidateClientMessage(message)
	})
}

func mustPayload(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := MarshalPayload(value)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	return payload
}
