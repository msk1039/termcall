package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/msk1039/termcall/internal/config"
	"github.com/msk1039/termcall/internal/signaling"
	termturn "github.com/msk1039/termcall/internal/turn"
	"golang.org/x/time/rate"
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		log.Printf("TermCall server stopped: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	serverConfig, err := config.LoadServer(args, ".env")
	if err != nil {
		return fmt.Errorf("load server configuration: %w", err)
	}

	turnServer, err := termturn.NewServer(termturn.Config{
		PublicIP:          serverConfig.PublicIP,
		Port:              serverConfig.TURNPort,
		Realm:             "termcall",
		SharedSecret:      serverConfig.TURNSharedSecret,
		CredentialTTL:     serverConfig.TURNCredentialTTL,
		MinRelayPort:      serverConfig.RelayPortMin,
		MaxRelayPort:      serverConfig.RelayPortMax,
		PerUserQuota:      serverConfig.TURNPerUserQuota,
		TotalQuota:        serverConfig.TURNTotalQuota,
		AllowPrivatePeers: serverConfig.AllowPrivatePeers,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := turnServer.Close(); err != nil {
			log.Printf("close TURN server: %v", err)
		}
	}()

	signalingServer, err := signaling.NewServer(signaling.ServerConfig{
		CredentialIssuer:    turnServer,
		MaxRoomSize:         serverConfig.MaxRoomSize,
		AllowedOrigins:      serverConfig.AllowedOrigins,
		MaxRooms:            serverConfig.MaxRooms,
		MaxConnections:      serverConfig.MaxConnections,
		MaxConnectionsPerIP: serverConfig.MaxConnectionsPerIP,
		MessageRate:         rate.Limit(serverConfig.MessageRate),
		MessageBurst:        serverConfig.MessageBurst,
	})
	if err != nil {
		return err
	}
	defer signalingServer.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", signalingServer.HandleWebSocket)
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", healthHandler)

	httpServer := &http.Server{
		Addr:              serverConfig.HTTPAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("signaling server is ready")
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutdown requested")
		signalingServer.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down signaling server: %w", err)
		}
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve signaling HTTP: %w", err)
	}
}

func healthHandler(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodGet {
		_, _ = response.Write([]byte("ok\n"))
	}
}
