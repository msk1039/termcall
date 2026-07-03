package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/msk1039/termcall/internal/signaling"
	"github.com/msk1039/termcall/internal/turn"
	turnPkg "github.com/pion/turn/v4"
)

func main() {
	port := flag.String("port", "8080", "HTTP listen port")
	turnPort := flag.Int("turn-port", 3478, "TURN server UDP port")
	turnIP := flag.String("turn-ip", "127.0.0.1", "TURN server public IP")
	flag.Parse()

	// Start STUN/TURN server
	_, err := turn.NewServer(turn.Config{
		PublicIP: *turnIP,
		Port:     *turnPort,
		Realm:    "termcall.local",
		Auth: func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
			// Accept any username with password "termcall" for now
			return turnPkg.GenerateAuthKey(username, realm, "termcall"), true
		},
	})
	turnSecret := "termcall"
	turnURLs := []string{
		"stun:stun.l.google.com:19302",
		fmt.Sprintf("stun:%s:%d", *turnIP, *turnPort),
		fmt.Sprintf("turn:%s:%d", *turnIP, *turnPort),
		fmt.Sprintf("turn:%s:%d?transport=tcp", *turnIP, *turnPort),
	}

	sigServer := signaling.NewServer(turnURLs, turnSecret)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", sigServer.HandleWebSocket)

	addr := fmt.Sprintf(":%s", *port)
	log.Printf("Starting TermCall signaling server on %s", addr)

	err = http.ListenAndServe(addr, mux)
	if err != nil {
		log.Printf("Server failed: %v", err)
		os.Exit(1)
	}
}
