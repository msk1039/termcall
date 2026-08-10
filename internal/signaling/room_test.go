package signaling

import (
	"context"
	"errors"
	"testing"

	"github.com/msk1039/termcall/internal/protocol"
)

func TestRoomManagerRejectsFullRoom(t *testing.T) {
	manager := NewRoomManager(10)
	room, _, err := manager.Join("new", 1, testPeer("one", 1, nil))
	if err != nil {
		t.Fatalf("first join: %v", err)
	}
	if room == nil {
		t.Fatal("first join returned no room")
	}
	if _, _, err := manager.Join(room.ID, 1, testPeer("two", 1, nil)); !errors.Is(err, ErrRoomFull) {
		t.Fatalf("second join error = %v", err)
	}
}

func TestStaleLeaveCannotDeleteReplacementRoom(t *testing.T) {
	manager := NewRoomManager(10)
	oldRoom, _, err := manager.Join("new", 2, testPeer("old", 1, nil))
	if err != nil {
		t.Fatalf("old join: %v", err)
	}
	manager.Leave(oldRoom, "old")

	newRoom, _, err := manager.Join("new", 2, testPeer("new", 1, nil))
	if err != nil {
		t.Fatalf("new join: %v", err)
	}
	manager.Leave(oldRoom, "old")

	if manager.RoomCount() != 1 {
		t.Fatalf("room count = %d", manager.RoomCount())
	}
	peers := manager.Peers(newRoom)
	if len(peers) != 1 || peers[0].PeerID != "new" {
		t.Fatalf("replacement peers = %#v", peers)
	}
}

func TestBroadcastCancelsSlowPeer(t *testing.T) {
	manager := NewRoomManager(10)
	canceled := make(chan struct{})
	room, _, err := manager.Join("new", 2, testPeer("slow", 0, func() { close(canceled) }))
	if err != nil {
		t.Fatalf("join: %v", err)
	}

	disconnected := manager.Broadcast(room, protocol.NewMessage(protocol.MsgPeerLeft), "")
	if disconnected != 1 {
		t.Fatalf("disconnected = %d", disconnected)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("slow peer was not canceled")
	}
}

func TestFailedAdmissionDoesNotExposeOrLeakPeer(t *testing.T) {
	manager := NewRoomManager(10)
	wantErr := errors.New("response queue failed")
	room, err := manager.Admit("new", 2, testPeer("one", 1, nil), func(string, []protocol.PeerInfo) error {
		return wantErr
	})
	if room != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Admit returned room=%v, error=%v", room, err)
	}
	if manager.RoomCount() != 0 {
		t.Fatalf("room count = %d", manager.RoomCount())
	}
}

func TestRoomManagerEnforcesGlobalRoomLimit(t *testing.T) {
	manager := NewRoomManager(1)
	room, _, err := manager.Join("new", 2, testPeer("one", 1, nil))
	if err != nil {
		t.Fatalf("first room: %v", err)
	}
	if _, _, err := manager.Join("new", 2, testPeer("two", 1, nil)); !errors.Is(err, ErrRoomLimit) {
		t.Fatalf("second room error = %v", err)
	}
	if _, _, err := manager.Join(room.ID, 2, testPeer("three", 1, nil)); err != nil {
		t.Fatalf("join existing room at room limit: %v", err)
	}
}

func TestRoomManagerDoesNotCreateRequestedRoomID(t *testing.T) {
	manager := NewRoomManager(10)
	_, _, err := manager.Join("2d54fa38-03a6-44af-9db6-0fd672d42f95", 2, testPeer("one", 1, nil))
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("unknown-room error = %v", err)
	}
	if manager.RoomCount() != 0 {
		t.Fatalf("room count = %d", manager.RoomCount())
	}
}

func testPeer(id string, queueSize int, onCancel func()) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	if onCancel != nil {
		baseCancel := cancel
		cancel = func() {
			onCancel()
			baseCancel()
		}
	}
	return &Peer{
		ID:     id,
		Send:   make(chan outboundMessage, queueSize),
		Ctx:    ctx,
		Cancel: cancel,
	}
}
