package signaling

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/msk1039/termcall/internal/protocol"
	"golang.org/x/time/rate"
)

const (
	peerQueueSize     = 256
	writeTimeout      = 10 * time.Second
	heartbeatInterval = 30 * time.Second
)

type CredentialIssuer interface {
	Issue(peerID string) (protocol.ICEServer, error)
}

type ServerConfig struct {
	CredentialIssuer    CredentialIssuer
	MaxRoomSize         int
	MaxRooms            int
	MaxConnections      int
	AllowedOrigins      []string
	MaxConnectionsPerIP int
	MessageRate         rate.Limit
	MessageBurst        int
}

type Server struct {
	roomManager *RoomManager
	config      ServerConfig
	ctx         context.Context
	cancel      context.CancelFunc

	connectionsMu    sync.Mutex
	connections      map[string]int
	totalConnections int
}

func NewServer(config ServerConfig) (*Server, error) {
	if config.CredentialIssuer == nil {
		return nil, errors.New("signaling credential issuer is required")
	}
	if config.MaxRoomSize < 2 {
		return nil, errors.New("signaling maximum room size must be at least 2")
	}
	if config.MaxRooms < 1 || config.MaxConnections < 1 {
		return nil, errors.New("signaling room and connection limits must be positive")
	}
	if config.MaxConnections < config.MaxConnectionsPerIP || config.MaxRooms > config.MaxConnections {
		return nil, errors.New("signaling global limits must cover the configured per-IP and room limits")
	}
	if config.MaxConnectionsPerIP < 1 {
		return nil, errors.New("signaling connection limit must be positive")
	}
	if config.MessageRate <= 0 || config.MessageBurst < 1 {
		return nil, errors.New("signaling message rate and burst must be positive")
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		roomManager: NewRoomManager(config.MaxRooms),
		config:      config,
		ctx:         ctx,
		cancel:      cancel,
		connections: map[string]int{},
	}, nil
}

func (s *Server) Close() {
	s.cancel()
}

func (s *Server) HandleWebSocket(response http.ResponseWriter, request *http.Request) {
	clientAddress := clientIP(request)
	if !s.acquireConnection(clientAddress) {
		http.Error(response, "too many signaling connections", http.StatusTooManyRequests)
		return
	}
	defer s.releaseConnection(clientAddress)

	connection, err := websocket.Accept(response, request, &websocket.AcceptOptions{
		OriginPatterns: s.config.AllowedOrigins,
	})
	if err != nil {
		log.Printf("rejected a signaling WebSocket handshake")
		return
	}
	connection.SetReadLimit(protocol.MaxSignalMessageBytes)

	ctx, cancel := context.WithCancel(s.ctx)
	stopRequestCancel := context.AfterFunc(request.Context(), cancel)
	defer func() {
		stopRequestCancel()
		cancel()
		_ = connection.Close(websocket.StatusNormalClosure, "")
	}()

	peer := &Peer{
		ID:     uuid.NewString(),
		Conn:   connection,
		Send:   make(chan outboundMessage, peerQueueSize),
		Ctx:    ctx,
		Cancel: cancel,
	}
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.writePump(peer)
	}()
	defer func() {
		cancel()
		<-writerDone
	}()

	messageLimiter := rate.NewLimiter(s.config.MessageRate, s.config.MessageBurst)
	var currentRoom *Room
	defer func() {
		if currentRoom != nil {
			s.leaveRoom(currentRoom, peer)
		}
	}()

	for {
		messageType, data, err := connection.Read(ctx)
		if err != nil {
			if !isDisconnect(err) {
				log.Printf("signaling WebSocket read failed")
			}
			return
		}
		if messageType != websocket.MessageText {
			s.sendFatalError(peer, "invalid_message", "signaling messages must be UTF-8 JSON text")
			return
		}
		message, err := protocol.DecodeMessage(data)
		if err != nil {
			s.sendFatalError(peer, "invalid_message", "signaling message is not valid protocol JSON")
			return
		}
		if !messageLimiter.Allow() {
			s.sendFatalError(peer, "rate_limited", "signaling message rate exceeded")
			return
		}
		if err := protocol.ValidateClientMessage(message); err != nil {
			code := "invalid_message"
			if errors.Is(err, protocol.ErrProtocolVersion) {
				code = "unsupported_version"
			}
			s.sendFatalError(peer, code, err.Error())
			return
		}

		switch message.Type {
		case protocol.MsgJoinRoom:
			if currentRoom != nil {
				s.sendError(peer, "already_joined", "leave the current room before joining another")
				continue
			}

			username, err := protocol.NormalizeUsername(message.Username)
			if err != nil {
				s.sendError(peer, "invalid_username", "username must contain printable characters")
				continue
			}
			peer.Username = username

			iceServer, err := s.config.CredentialIssuer.Issue(peer.ID)
			if err != nil {
				s.sendError(peer, "turn_unavailable", "relay credentials are unavailable")
				continue
			}

			room, err := s.roomManager.Admit(message.RoomID, s.config.MaxRoomSize, peer, func(roomID string, existingPeers []protocol.PeerInfo) error {
				payload, err := protocol.MarshalPayload(protocol.JoinAcceptedPayload{
					RoomID:     roomID,
					PeerID:     peer.ID,
					Peers:      existingPeers,
					ICEServers: []protocol.ICEServer{iceServer},
				})
				if err != nil {
					return err
				}

				accepted := protocol.NewMessage(protocol.MsgJoinAccepted)
				accepted.RoomID = roomID
				accepted.PeerID = peer.ID
				accepted.Payload = payload
				return peer.enqueue(accepted)
			})
			if err != nil {
				if errors.Is(err, ErrRoomFull) {
					s.sendError(peer, "room_full", "room is full")
					continue
				}
				if errors.Is(err, ErrRoomLimit) {
					s.sendError(peer, "server_busy", "the server has reached its room limit")
					continue
				}
				if errors.Is(err, ErrRoomNotFound) {
					s.sendError(peer, "room_not_found", "room does not exist")
					continue
				}
				s.sendError(peer, "join_failed", "could not join room")
				continue
			}
			currentRoom = room

			joined := protocol.NewMessage(protocol.MsgPeerJoined)
			joined.PeerID = peer.ID
			joined.Username = peer.Username
			s.roomManager.Broadcast(room, joined, peer.ID)

		case protocol.MsgOffer, protocol.MsgAnswer, protocol.MsgICECandidate:
			if currentRoom == nil {
				s.sendError(peer, "not_joined", "join a room before signaling a peer")
				continue
			}
			targetPeerID := message.PeerID
			message.Version = protocol.Version
			message.PeerID = peer.ID
			message.RoomID = ""
			message.Username = ""
			if err := s.roomManager.Route(currentRoom, targetPeerID, message); err != nil {
				s.sendError(peer, "peer_unavailable", "target peer is unavailable")
			}

		case protocol.MsgLeaveRoom:
			if currentRoom != nil {
				s.leaveRoom(currentRoom, peer)
				currentRoom = nil
			}
		}
	}
}

func (s *Server) writePump(peer *Peer) {
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case outbound := <-peer.Send:
			ctx, cancel := context.WithTimeout(peer.Ctx, writeTimeout)
			err := wsjson.Write(ctx, peer.Conn, outbound.message)
			cancel()
			if outbound.result != nil {
				outbound.result <- err
			}
			if err != nil {
				peer.Cancel()
				return
			}
		case <-heartbeat.C:
			ctx, cancel := context.WithTimeout(peer.Ctx, writeTimeout)
			err := peer.Conn.Ping(ctx)
			cancel()
			if err != nil {
				peer.Cancel()
				return
			}
		case <-peer.Ctx.Done():
			return
		}
	}
}

func (s *Server) leaveRoom(room *Room, peer *Peer) {
	remaining := s.roomManager.Leave(room, peer.ID)
	left := protocol.NewMessage(protocol.MsgPeerLeft)
	left.PeerID = peer.ID
	for _, remainingPeer := range remaining {
		_ = remainingPeer.enqueue(left)
	}
}

func (s *Server) sendError(peer *Peer, code, message string) {
	response, err := errorMessage(code, message)
	if err != nil {
		peer.Cancel()
		return
	}
	_ = peer.enqueue(response)
}

func (s *Server) sendFatalError(peer *Peer, code, message string) {
	response, err := errorMessage(code, message)
	if err != nil {
		peer.Cancel()
		return
	}
	ctx, cancel := context.WithTimeout(peer.Ctx, writeTimeout)
	defer cancel()
	_ = peer.enqueueAndWait(ctx, response)
}

func errorMessage(code, message string) (protocol.SignalingMessage, error) {
	payload, err := protocol.MarshalPayload(protocol.ErrorPayload{
		Code:    code,
		Message: message,
	})
	if err != nil {
		return protocol.SignalingMessage{}, err
	}
	response := protocol.NewMessage(protocol.MsgError)
	response.Payload = payload
	return response, nil
}

func (s *Server) acquireConnection(clientAddress string) bool {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	if s.totalConnections >= s.config.MaxConnections || s.connections[clientAddress] >= s.config.MaxConnectionsPerIP {
		return false
	}
	s.connections[clientAddress]++
	s.totalConnections++
	return true
}

func (s *Server) releaseConnection(clientAddress string) {
	s.connectionsMu.Lock()
	defer s.connectionsMu.Unlock()
	if s.connections[clientAddress] <= 1 {
		delete(s.connections, clientAddress)
	} else {
		s.connections[clientAddress]--
	}
	if s.totalConnections > 0 {
		s.totalConnections--
	}
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	remoteIP := net.ParseIP(host)
	if remoteIP == nil || !remoteIP.IsLoopback() {
		return host
	}

	forwarded := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
	if len(forwarded) == 0 {
		return host
	}
	candidate := strings.TrimSpace(forwarded[0])
	if net.ParseIP(candidate) == nil {
		return host
	}
	return candidate
}

func isDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if websocket.CloseStatus(err) != -1 {
		return true
	}
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET)
}
