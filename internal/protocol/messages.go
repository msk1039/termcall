package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const (
	Version               = 1
	MaxSignalMessageBytes = 64 * 1024
	MaxPayloadBytes       = 48 * 1024
	MaxUsernameRunes      = 32
	MaxRoomIDBytes        = 128
	MaxPeerIDBytes        = 64
	MaxSDPBytes           = 32 * 1024
	MaxICECandidateBytes  = 8 * 1024
	MaxSDPMidBytes        = 256
	MaxUsernameFragBytes  = 256
	MaxErrorCodeBytes     = 64
	MaxErrorMessageBytes  = 512
)

var (
	ErrProtocolVersion = errors.New("unsupported protocol version")
	ErrInvalidMessage  = errors.New("invalid signaling message")
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidRoomID   = errors.New("invalid room ID")
	ErrInvalidPeerID   = errors.New("invalid peer ID")
)

type MessageType string

const (
	MsgJoinRoom     MessageType = "join_room"
	MsgJoinAccepted MessageType = "join_accepted"
	MsgLeaveRoom    MessageType = "leave_room"
	MsgPeerJoined   MessageType = "peer_joined"
	MsgPeerLeft     MessageType = "peer_left"
	MsgOffer        MessageType = "offer"
	MsgAnswer       MessageType = "answer"
	MsgICECandidate MessageType = "ice_candidate"
	MsgError        MessageType = "error"
)

type SignalingMessage struct {
	Version  int             `json:"version"`
	Type     MessageType     `json:"type"`
	RoomID   string          `json:"room_id,omitempty"`
	PeerID   string          `json:"peer_id,omitempty"`
	Username string          `json:"username,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type PeerInfo struct {
	PeerID   string `json:"peer_id"`
	Username string `json:"username"`
}

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
	ExpiresAt  int64    `json:"expires_at,omitempty"`
}

type JoinAcceptedPayload struct {
	RoomID     string      `json:"room_id"`
	PeerID     string      `json:"peer_id"`
	Peers      []PeerInfo  `json:"peers"`
	ICEServers []ICEServer `json:"ice_servers"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SDPPayload struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"`
}

type ICECandidatePayload struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

func NewMessage(messageType MessageType) SignalingMessage {
	return SignalingMessage{
		Version: Version,
		Type:    messageType,
	}
}

func DecodeMessage(data []byte) (SignalingMessage, error) {
	var message SignalingMessage
	if len(data) == 0 || len(data) > MaxSignalMessageBytes {
		return message, fmt.Errorf("%w: invalid message size", ErrInvalidMessage)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return message, fmt.Errorf("%w: decode message: %v", ErrInvalidMessage, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return message, fmt.Errorf("%w: message has trailing data", ErrInvalidMessage)
	}
	return message, nil
}

func MarshalPayload(value any) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal signaling payload: %w", err)
	}
	if len(payload) > MaxPayloadBytes {
		return nil, fmt.Errorf("%w: payload exceeds %d bytes", ErrInvalidMessage, MaxPayloadBytes)
	}
	return payload, nil
}

func DecodePayload[T any](payload json.RawMessage) (T, error) {
	var value T
	if len(payload) == 0 || len(payload) > MaxPayloadBytes {
		return value, fmt.Errorf("%w: invalid payload size", ErrInvalidMessage)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("%w: decode payload: %v", ErrInvalidMessage, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return value, fmt.Errorf("%w: payload has trailing data", ErrInvalidMessage)
	}
	return value, nil
}

func ValidateClientMessage(message SignalingMessage) error {
	if message.Version != Version {
		return ErrProtocolVersion
	}
	if len(message.Payload) > MaxPayloadBytes {
		return fmt.Errorf("%w: payload too large", ErrInvalidMessage)
	}
	encoded, err := json.Marshal(message)
	if err != nil || len(encoded) > MaxSignalMessageBytes {
		return fmt.Errorf("%w: message exceeds the transport limit", ErrInvalidMessage)
	}

	switch message.Type {
	case MsgJoinRoom:
		if message.PeerID != "" || len(message.Payload) != 0 {
			return fmt.Errorf("%w: join contains unexpected fields", ErrInvalidMessage)
		}
		if err := ValidateRoomID(message.RoomID); err != nil {
			return err
		}
		_, err := NormalizeUsername(message.Username)
		return err
	case MsgLeaveRoom:
		if message.RoomID != "" || message.PeerID != "" || message.Username != "" || len(message.Payload) != 0 {
			return fmt.Errorf("%w: leave contains unexpected fields", ErrInvalidMessage)
		}
		return nil
	case MsgOffer, MsgAnswer, MsgICECandidate:
		if message.RoomID != "" || message.Username != "" {
			return fmt.Errorf("%w: signal contains unexpected fields", ErrInvalidMessage)
		}
		if err := ValidatePeerID(message.PeerID); err != nil {
			return err
		}
		return ValidateSignalPayload(message.Type, message.Payload)
	default:
		return fmt.Errorf("%w: client message type %q", ErrInvalidMessage, message.Type)
	}
}

// ValidateServerMessage checks messages delivered after admission. Join
// responses are validated synchronously by the signaling client.
func ValidateServerMessage(message SignalingMessage) error {
	if message.Version != Version {
		return ErrProtocolVersion
	}
	if len(message.Payload) > MaxPayloadBytes {
		return fmt.Errorf("%w: payload too large", ErrInvalidMessage)
	}

	switch message.Type {
	case MsgPeerJoined:
		if err := ValidatePeerID(message.PeerID); err != nil {
			return err
		}
		_, err := NormalizeUsername(message.Username)
		return err
	case MsgPeerLeft:
		return ValidatePeerID(message.PeerID)
	case MsgOffer, MsgAnswer, MsgICECandidate:
		if err := ValidatePeerID(message.PeerID); err != nil {
			return err
		}
		return ValidateSignalPayload(message.Type, message.Payload)
	case MsgError:
		failure, err := DecodePayload[ErrorPayload](message.Payload)
		if err != nil {
			return err
		}
		if !validErrorCode(failure.Code) || len(failure.Message) == 0 || len(failure.Message) > MaxErrorMessageBytes || hasControl(failure.Message) {
			return fmt.Errorf("%w: invalid error payload", ErrInvalidMessage)
		}
		return nil
	default:
		return fmt.Errorf("%w: server message type %q", ErrInvalidMessage, message.Type)
	}
}

func ValidateSignalPayload(messageType MessageType, payload json.RawMessage) error {
	switch messageType {
	case MsgOffer, MsgAnswer:
		session, err := DecodePayload[SDPPayload](payload)
		if err != nil {
			return err
		}
		expectedType := string(messageType)
		if session.Type != expectedType || len(session.SDP) == 0 || len(session.SDP) > MaxSDPBytes || strings.ContainsRune(session.SDP, '\x00') {
			return fmt.Errorf("%w: invalid SDP payload", ErrInvalidMessage)
		}
		return nil
	case MsgICECandidate:
		candidate, err := DecodePayload[ICECandidatePayload](payload)
		if err != nil {
			return err
		}
		if len(candidate.Candidate) == 0 || len(candidate.Candidate) > MaxICECandidateBytes || hasControl(candidate.Candidate) {
			return fmt.Errorf("%w: invalid ICE candidate", ErrInvalidMessage)
		}
		if candidate.SDPMid != nil && (len(*candidate.SDPMid) > MaxSDPMidBytes || hasControl(*candidate.SDPMid)) {
			return fmt.Errorf("%w: SDP mid is too long", ErrInvalidMessage)
		}
		if candidate.UsernameFragment != nil && (len(*candidate.UsernameFragment) > MaxUsernameFragBytes || hasControl(*candidate.UsernameFragment)) {
			return fmt.Errorf("%w: ICE username fragment is too long", ErrInvalidMessage)
		}
		return nil
	default:
		return fmt.Errorf("%w: message type %q has no signal payload", ErrInvalidMessage, messageType)
	}
}

func NormalizeUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrInvalidUsername
	}

	var builder strings.Builder
	count := 0
	for _, char := range value {
		if unicode.IsControl(char) || !unicode.IsPrint(char) {
			continue
		}
		if count == MaxUsernameRunes {
			break
		}
		builder.WriteRune(char)
		count++
	}
	normalized := strings.TrimSpace(builder.String())
	if normalized == "" {
		return "", ErrInvalidUsername
	}
	return normalized, nil
}

func ValidateRoomID(roomID string) error {
	if roomID == "new" {
		return nil
	}
	if len(roomID) != 36 || len(roomID) > MaxRoomIDBytes {
		return ErrInvalidRoomID
	}
	for index, char := range roomID {
		isHyphenPosition := index == 8 || index == 13 || index == 18 || index == 23
		if isHyphenPosition {
			if char != '-' {
				return ErrInvalidRoomID
			}
			continue
		}
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return ErrInvalidRoomID
		}
	}
	return nil
}

func ValidatePeerID(peerID string) error {
	if peerID == "" || len(peerID) > MaxPeerIDBytes {
		return ErrInvalidPeerID
	}
	for _, char := range peerID {
		if !isIdentifierRune(char) {
			return ErrInvalidPeerID
		}
	}
	return nil
}

func isIdentifierRune(char rune) bool {
	return unicode.IsLetter(char) || unicode.IsDigit(char) || unicode.IsMark(char) || char == '-' || char == '_' || char == '.'
}

func validErrorCode(value string) bool {
	if value == "" || len(value) > MaxErrorCodeBytes {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func hasControl(value string) bool {
	for _, char := range value {
		if unicode.IsControl(char) {
			return true
		}
	}
	return false
}
