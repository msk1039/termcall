package rtc

import "github.com/meow/termcall/internal/protocol"

// SignalTransport defines how the client talks to the signaling server.
type SignalTransport interface {
	Send(msg protocol.SignalingMessage) error
	Receive() <-chan protocol.SignalingMessage
	Close() error
}
