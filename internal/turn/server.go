package turn

import (
	"fmt"
	"log"
	"net"

	"github.com/pion/logging"
	"github.com/pion/turn/v4"
)

type Server struct {
	turnServer *turn.Server
}

// Config for TURN server
type Config struct {
	PublicIP string
	Port     int
	Realm    string
	Auth     turn.AuthHandler
}

// NewServer creates a new STUN/TURN server
func NewServer(cfg Config) (*Server, error) {
	udpListener, err := net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to create TURN UDP listener: %w", err)
	}

	tcpListener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to create TURN TCP listener: %w", err)
	}

	log.Printf("Starting STUN/TURN server on UDP %d (Public IP: %s)", cfg.Port, cfg.PublicIP)

	s, err := turn.NewServer(turn.ServerConfig{
		Realm:         cfg.Realm,
		AuthHandler:   cfg.Auth,
		LoggerFactory: logging.NewDefaultLoggerFactory(),
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{
					RelayAddress: net.ParseIP(cfg.PublicIP),
					Address:      "0.0.0.0",
					MinPort:      50000,
					MaxPort:      50050,
				},
			},
		},
		ListenerConfigs: []turn.ListenerConfig{
			{
				Listener: tcpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{
					RelayAddress: net.ParseIP(cfg.PublicIP),
					Address:      "0.0.0.0",
					MinPort:      50000,
					MaxPort:      50050,
				},
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create turn server: %w", err)
	}

	return &Server{turnServer: s}, nil
}

func (s *Server) Close() error {
	return s.turnServer.Close()
}
