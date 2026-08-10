package signaling

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/msk1039/termcall/internal/protocol"
)

var (
	ErrRoomFull      = errors.New("room is full")
	ErrPeerNotFound  = errors.New("peer not found")
	ErrSignalingSlow = errors.New("peer signaling queue is full")
	ErrRoomLimit     = errors.New("server room limit reached")
	ErrRoomNotFound  = errors.New("room not found")
)

type Peer struct {
	ID       string
	Username string
	Conn     *websocket.Conn
	Send     chan outboundMessage
	Ctx      context.Context
	Cancel   context.CancelFunc
}

type outboundMessage struct {
	message protocol.SignalingMessage
	result  chan error
}

func (p *Peer) enqueue(message protocol.SignalingMessage) error {
	select {
	case p.Send <- outboundMessage{message: message}:
		return nil
	default:
		p.Cancel()
		return ErrSignalingSlow
	}
}

func (p *Peer) enqueueAndWait(ctx context.Context, message protocol.SignalingMessage) error {
	result := make(chan error, 1)
	select {
	case p.Send <- outboundMessage{message: message, result: result}:
	case <-ctx.Done():
		return ctx.Err()
	case <-p.Ctx.Done():
		return p.Ctx.Err()
	default:
		p.Cancel()
		return ErrSignalingSlow
	}

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-p.Ctx.Done():
		return p.Ctx.Err()
	}
}

type Room struct {
	ID        string
	CreatedAt time.Time
	MaxPeers  int
	peers     map[string]*Peer
}

type RoomManager struct {
	rooms    map[string]*Room
	maxRooms int
	mu       sync.RWMutex
}

func NewRoomManager(maxRooms int) *RoomManager {
	return &RoomManager{
		rooms:    map[string]*Room{},
		maxRooms: maxRooms,
	}
}

func (manager *RoomManager) Join(requestedID string, maxPeers int, peer *Peer) (*Room, []protocol.PeerInfo, error) {
	var existing []protocol.PeerInfo
	room, err := manager.Admit(requestedID, maxPeers, peer, func(_ string, peers []protocol.PeerInfo) error {
		existing = peers
		return nil
	})
	return room, existing, err
}

// Admit commits a peer only after its admission response has been prepared and
// queued. The callback runs under the manager lock and must not block on I/O.
func (manager *RoomManager) Admit(
	requestedID string,
	maxPeers int,
	peer *Peer,
	accept func(roomID string, existing []protocol.PeerInfo) error,
) (*Room, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	room, exists := manager.rooms[requestedID]
	if requestedID == "new" {
		if len(manager.rooms) >= manager.maxRooms {
			return nil, ErrRoomLimit
		}
		roomID := uuid.NewString()
		room = &Room{
			ID:        roomID,
			CreatedAt: time.Now(),
			MaxPeers:  maxPeers,
			peers:     map[string]*Peer{},
		}
		manager.rooms[roomID] = room
	} else if !exists {
		return nil, ErrRoomNotFound
	}
	if len(room.peers) >= room.MaxPeers {
		return nil, fmt.Errorf("%w (maximum %d peers)", ErrRoomFull, room.MaxPeers)
	}

	existingPeers := peerInfoSnapshot(room.peers, "")
	if accept != nil {
		if err := accept(room.ID, existingPeers); err != nil {
			if len(room.peers) == 0 {
				delete(manager.rooms, room.ID)
			}
			return nil, err
		}
	}
	room.peers[peer.ID] = peer
	return room, nil
}

// Leave atomically removes a peer and deletes the same room instance when it
// becomes empty. A stale handler cannot delete a newly created replacement.
func (manager *RoomManager) Leave(room *Room, peerID string) []*Peer {
	if room == nil {
		return []*Peer{}
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

	current, exists := manager.rooms[room.ID]
	if !exists || current != room {
		return []*Peer{}
	}
	delete(current.peers, peerID)
	remaining := peerSnapshot(current.peers, "")
	if len(current.peers) == 0 {
		delete(manager.rooms, current.ID)
	}
	return remaining
}

func (manager *RoomManager) Broadcast(
	room *Room,
	message protocol.SignalingMessage,
	excludePeerID string,
) int {
	peers := manager.roomPeers(room, excludePeerID)
	disconnected := 0
	for _, peer := range peers {
		if err := peer.enqueue(message); err != nil {
			disconnected++
		}
	}
	return disconnected
}

func (manager *RoomManager) Route(room *Room, targetPeerID string, message protocol.SignalingMessage) error {
	manager.mu.RLock()
	current, exists := manager.rooms[room.ID]
	if !exists || current != room {
		manager.mu.RUnlock()
		return ErrPeerNotFound
	}
	target, exists := current.peers[targetPeerID]
	manager.mu.RUnlock()
	if !exists {
		return fmt.Errorf("%w: %s", ErrPeerNotFound, targetPeerID)
	}
	return target.enqueue(message)
}

func (manager *RoomManager) Peers(room *Room) []protocol.PeerInfo {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	current, exists := manager.rooms[room.ID]
	if !exists || current != room {
		return []protocol.PeerInfo{}
	}
	return peerInfoSnapshot(current.peers, "")
}

func (manager *RoomManager) RoomCount() int {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return len(manager.rooms)
}

func (manager *RoomManager) roomPeers(room *Room, excludePeerID string) []*Peer {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	current, exists := manager.rooms[room.ID]
	if !exists || current != room {
		return []*Peer{}
	}
	return peerSnapshot(current.peers, excludePeerID)
}

func peerSnapshot(peers map[string]*Peer, excludePeerID string) []*Peer {
	result := make([]*Peer, 0, len(peers))
	for peerID, peer := range peers {
		if peerID != excludePeerID {
			result = append(result, peer)
		}
	}
	return result
}

func peerInfoSnapshot(peers map[string]*Peer, excludePeerID string) []protocol.PeerInfo {
	result := make([]protocol.PeerInfo, 0, len(peers))
	for peerID, peer := range peers {
		if peerID == excludePeerID {
			continue
		}
		result = append(result, protocol.PeerInfo{
			PeerID:   peer.ID,
			Username: peer.Username,
		})
	}
	return result
}
