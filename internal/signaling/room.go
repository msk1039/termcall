package signaling

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meow/termcall/internal/protocol"
	"nhooyr.io/websocket"
)

type Peer struct {
	ID       string
	Username string
	Conn     *websocket.Conn
	Send     chan protocol.SignalingMessage
	Ctx      context.Context
	Cancel   context.CancelFunc
}

type Room struct {
	ID        string
	Peers     map[string]*Peer
	mu        sync.RWMutex
	CreatedAt time.Time
	MaxPeers  int
}

type RoomManager struct {
	rooms map[string]*Room
	mu    sync.RWMutex
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Room),
	}
}

// GetOrCreateRoom returns an existing room or creates a new one
func (rm *RoomManager) GetOrCreateRoom(id string, maxPeers int) *Room {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if id == "new" || id == "" {
		id = uuid.New().String()[:8]
	}

	room, exists := rm.rooms[id]
	if !exists {
		room = &Room{
			ID:        id,
			Peers:     make(map[string]*Peer),
			CreatedAt: time.Now(),
			MaxPeers:  maxPeers,
		}
		rm.rooms[id] = room
	}

	return room
}

// RemoveRoom deletes a room if it exists
func (rm *RoomManager) RemoveRoom(id string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.rooms, id)
}

// AddPeer adds a peer to the room. Returns an error if room is full.
func (r *Room) AddPeer(peer *Peer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.Peers) >= r.MaxPeers {
		return fmt.Errorf("room is full (max %d peers)", r.MaxPeers)
	}

	r.Peers[peer.ID] = peer
	return nil
}

// RemovePeer removes a peer from the room. Returns the remaining number of peers.
func (r *Room) RemovePeer(peerID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.Peers, peerID)
	return len(r.Peers)
}

// Broadcast sends a message to all peers in the room EXCEPT the specified excludePeerID.
func (r *Room) Broadcast(msg protocol.SignalingMessage, excludePeerID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, peer := range r.Peers {
		if id != excludePeerID {
			select {
			case peer.Send <- msg:
			default:
				// If the send channel is full, drop the message or handle it
				// For signaling, channel shouldn't be full unless client is dead
			}
		}
	}
}

// RouteMessage sends a message to a specific peer in the room.
func (r *Room) RouteMessage(targetPeerID string, msg protocol.SignalingMessage) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	target, exists := r.Peers[targetPeerID]
	if !exists {
		return fmt.Errorf("peer %s not found in room", targetPeerID)
	}

	select {
	case target.Send <- msg:
		return nil
	default:
		return fmt.Errorf("peer %s send channel full", targetPeerID)
	}
}

// GetPeers returns a snapshot of the current peers
func (r *Room) GetPeers() []protocol.SignalingMessage {
	r.mu.RLock()
	defer r.mu.RUnlock()

	peers := make([]protocol.SignalingMessage, 0, len(r.Peers))
	for _, p := range r.Peers {
		peers = append(peers, protocol.SignalingMessage{
			Type:     protocol.MsgPeerJoined,
			PeerID:   p.ID,
			Username: p.Username,
		})
	}
	return peers
}
