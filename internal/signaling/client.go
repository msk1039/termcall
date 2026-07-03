package signaling

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/meow/termcall/internal/protocol"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Client struct {
	conn *websocket.Conn
	recv chan protocol.SignalingMessage
	mu   sync.Mutex
	ctx  context.Context
}

// NewClient connects to the signaling server and starts reading messages.
func NewClient(ctx context.Context, url string, roomID string, username string) (*Client, error) {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial signaling server: %w", err)
	}

	client := &Client{
		conn: c,
		recv: make(chan protocol.SignalingMessage, 256),
		ctx:  ctx,
	}

	// Send initial JoinRoom message
	err = wsjson.Write(ctx, c, protocol.SignalingMessage{
		Type:     protocol.MsgJoinRoom,
		RoomID:   roomID,
		Username: username,
	})
	if err != nil {
		c.Close(websocket.StatusInternalError, "failed to send join")
		return nil, fmt.Errorf("failed to send join message: %w", err)
	}

	go client.readPump()

	return client, nil
}

func (c *Client) Send(msg protocol.SignalingMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return wsjson.Write(c.ctx, c.conn, msg)
}

func (c *Client) Receive() <-chan protocol.SignalingMessage {
	return c.recv
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Send leave message
	wsjson.Write(c.ctx, c.conn, protocol.SignalingMessage{
		Type: protocol.MsgLeaveRoom,
	})
	
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

func (c *Client) readPump() {
	defer close(c.recv)
	for {
		var msg protocol.SignalingMessage
		err := wsjson.Read(c.ctx, c.conn, &msg)
		if err != nil {
			log.Printf("Disconnected from signaling server: %v", err)
			return
		}

		select {
		case c.recv <- msg:
		case <-c.ctx.Done():
			return
		}
	}
}
