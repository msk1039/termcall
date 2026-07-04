package signaling

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/msk1039/termcall/internal/protocol"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Server struct {
	roomManager *RoomManager
	turnURLs    []string
	turnUser    string
	turnSecret  string
	maxRoomSize int
}

func NewServer(turnURLs []string, turnSecret string, maxRoomSize int) *Server {
	return &Server{
		roomManager: NewRoomManager(),
		turnURLs:    turnURLs,
		turnSecret:  turnSecret,
		maxRoomSize: maxRoomSize,
	}
}

func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow cross-origin for testing
	})
	if err != nil {
		log.Printf("Failed to accept websocket: %v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	peerID := uuid.New().String()[:8]
	peer := &Peer{
		ID:     peerID,
		Conn:   c,
		Send:   make(chan protocol.SignalingMessage, 256),
		Ctx:    ctx,
		Cancel: cancel,
	}

	var currentRoom *Room

	// Start writer goroutine
	go s.writePump(peer)

	for {
		var msg protocol.SignalingMessage
		err := wsjson.Read(ctx, c, &msg)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				log.Printf("Error reading message from peer %s: %v", peerID, err)
			}
			break
		}

		switch msg.Type {
		case protocol.MsgJoinRoom:
			if currentRoom != nil {
				s.sendError(peer, "Already in a room")
				continue
			}

			peer.Username = msg.Username
			if peer.Username == "" {
				peer.Username = "Guest-" + peer.ID
			}

			room := s.roomManager.GetOrCreateRoom(msg.RoomID, s.maxRoomSize)
			err := room.AddPeer(peer)
			if err != nil {
				s.sendError(peer, err.Error())
				continue
			}
			currentRoom = room

			// Send room created/joined confirmation
			peer.Send <- protocol.SignalingMessage{
				Type:   protocol.MsgRoomCreated,
				RoomID: room.ID,
				PeerID: peer.ID,
			}

			// Send TURN credentials
			peer.Send <- protocol.SignalingMessage{
				Type: protocol.MsgTURNCredentials,
				Payload: toJSON(protocol.TURNCredentials{
					URLs:       s.turnURLs,
					Username:   peer.ID,
					Credential: s.turnSecret,
				}),
			}

			// Tell existing peers about the new peer
			room.Broadcast(protocol.SignalingMessage{
				Type:     protocol.MsgPeerJoined,
				PeerID:   peer.ID,
				Username: peer.Username,
			}, peer.ID)

			// Tell new peer about existing peers
			for _, p := range room.GetPeers() {
				if p.PeerID != peer.ID {
					peer.Send <- p
				}
			}

		case protocol.MsgOffer, protocol.MsgAnswer, protocol.MsgICECandidate:
			if currentRoom == nil {
				s.sendError(peer, "Not in a room")
				continue
			}
			// Route to specific peer
			targetPeerID := msg.PeerID
			msg.PeerID = peer.ID // Source peer ID
			err := currentRoom.RouteMessage(targetPeerID, msg)
			if err != nil {
				log.Printf("Failed to route message: %v", err)
			}

		case protocol.MsgLeaveRoom:
			if currentRoom != nil {
				rem := currentRoom.RemovePeer(peer.ID)
				currentRoom.Broadcast(protocol.SignalingMessage{
					Type:   protocol.MsgPeerLeft,
					PeerID: peer.ID,
				}, "")
				if rem == 0 {
					s.roomManager.RemoveRoom(currentRoom.ID)
				}
				currentRoom = nil
			}
		}
	}

	// Cleanup on disconnect
	if currentRoom != nil {
		rem := currentRoom.RemovePeer(peer.ID)
		currentRoom.Broadcast(protocol.SignalingMessage{
			Type:   protocol.MsgPeerLeft,
			PeerID: peer.ID,
		}, "")
		if rem == 0 {
			s.roomManager.RemoveRoom(currentRoom.ID)
		}
	}
	c.Close(websocket.StatusNormalClosure, "")
}

func (s *Server) writePump(peer *Peer) {
	for {
		select {
		case msg := <-peer.Send:
			err := wsjson.Write(peer.Ctx, peer.Conn, msg)
			if err != nil {
				log.Printf("Error writing message to peer %s: %v", peer.ID, err)
				peer.Cancel() // Cancel context to kill read loop
				return
			}
		case <-peer.Ctx.Done():
			return
		}
	}
}

func (s *Server) sendError(peer *Peer, errorMsg string) {
	peer.Send <- protocol.SignalingMessage{
		Type:    protocol.MsgError,
		Payload: toJSON(errorMsg),
	}
}

func toJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
