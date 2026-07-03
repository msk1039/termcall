package protocol

import "encoding/json"

type MessageType string

const (
	MsgJoinRoom        MessageType = "join_room"
	MsgLeaveRoom       MessageType = "leave_room"
	MsgPeerJoined      MessageType = "peer_joined"
	MsgPeerLeft        MessageType = "peer_left"
	MsgOffer           MessageType = "offer"
	MsgAnswer          MessageType = "answer"
	MsgICECandidate    MessageType = "ice_candidate"
	MsgRoomCreated     MessageType = "room_created"
	MsgError           MessageType = "error"
	MsgTURNCredentials MessageType = "turn_credentials"
)

type SignalingMessage struct {
	Type     MessageType     `json:"type"`
	RoomID   string          `json:"room_id,omitempty"`
	PeerID   string          `json:"peer_id,omitempty"`
	Username string          `json:"username,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type SDPPayload struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"` // "offer" or "answer"
}

type ICECandidatePayload struct {
	Candidate     string  `json:"candidate"`
	SDPMid        *string `json:"sdpMid"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

type TURNCredentials struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}
