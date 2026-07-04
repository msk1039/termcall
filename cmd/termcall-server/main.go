package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/msk1039/termcall/internal/signaling"
	"github.com/msk1039/termcall/internal/turn"
	turnPkg "github.com/pion/turn/v4"
)

func main() {
	loadEnvFile(".env")

	// CLI flags — defaults come from env vars (which may come from .env)
	port := flag.String("port", envOrDefault("TERMCALL_WS_PORT", "8080"), "HTTP listen port")
	turnPort := flag.Int("turn-port", envInt("TERMCALL_TURN_PORT", 3478), "TURN server listen port (UDP+TCP)")
	turnIP := flag.String("turn-ip", envOrDefault("TERMCALL_PUBLIC_IP", ""), "TURN server public IP (required)")
	relayMin := flag.Int("relay-min", envInt("TERMCALL_RELAY_PORT_MIN", 49152), "TURN relay port range start")
	relayMax := flag.Int("relay-max", envInt("TERMCALL_RELAY_PORT_MAX", 65535), "TURN relay port range end")
	maxRoom := flag.Int("max-room-size", envInt("TERMCALL_MAX_ROOM_SIZE", 5), "Maximum peers per room")
	turnSecret := flag.String("turn-secret", envOrDefault("TERMCALL_TURN_SECRET", "termcall"), "TURN shared secret")
	flag.Parse()

	// Validate required config
	if *turnIP == "" {
		fmt.Fprintln(os.Stderr, "Error: public IP is required.")
		fmt.Fprintln(os.Stderr, "Set TERMCALL_PUBLIC_IP in .env or pass --turn-ip <ip>")
		fmt.Fprintln(os.Stderr, "Find your public IP with: curl -s ifconfig.me")
		os.Exit(1)
	}

	if *relayMin > *relayMax {
		fmt.Fprintf(os.Stderr, "Error: relay-min (%d) must be <= relay-max (%d)\n", *relayMin, *relayMax)
		os.Exit(1)
	}

	// Start STUN/TURN server
	_, err := turn.NewServer(turn.Config{
		PublicIP:     *turnIP,
		Port:         *turnPort,
		Realm:        "termcall",
		MinRelayPort: uint16(*relayMin),
		MaxRelayPort: uint16(*relayMax),
		Auth: func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
			return turnPkg.GenerateAuthKey(username, realm, *turnSecret), true
		},
	})
	if err != nil {
		log.Fatalf("Failed to start TURN server: %v", err)
	}

	turnURLs := []string{
		fmt.Sprintf("stun:%s:%d", *turnIP, *turnPort),
		fmt.Sprintf("turn:%s:%d", *turnIP, *turnPort),
		fmt.Sprintf("turn:%s:%d?transport=tcp", *turnIP, *turnPort),
	}

	sigServer := signaling.NewServer(turnURLs, *turnSecret, *maxRoom)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", sigServer.HandleWebSocket)

	addr := fmt.Sprintf(":%s", *port)
	log.Printf("TermCall server starting")
	log.Printf("  WebSocket:    %s", addr)
	log.Printf("  TURN/STUN:    :%d (UDP+TCP)", *turnPort)
	log.Printf("  Relay range:  %d–%d (UDP)", *relayMin, *relayMax)
	log.Printf("  Public IP:    %s", *turnIP)
	log.Printf("  Max room:     %d peers", *maxRoom)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // .env is optional
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Only set if not already set (real env vars / CLI take priority)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Warning: failed reading .env: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
