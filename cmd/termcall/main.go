package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/msk1039/termcall/internal/capture"
	"github.com/msk1039/termcall/internal/rtc"
	"github.com/msk1039/termcall/internal/signaling"
	"github.com/msk1039/termcall/internal/tui"
)

func main() {
	serverURL := flag.String("server", "", "Signaling server URL (optional, skips form if room and username also provided)")
	roomID := flag.String("room", "", "Room ID (optional, skips form if server and username also provided)")
	username := flag.String("username", "", "Username (optional, skips form if server and room also provided)")
	flag.Parse()

	// ...
	// Setup context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		log.Fatalf("fatal: %v", err)
	}
	defer f.Close()

	skipForm := *serverURL != "" && *roomID != "" && *username != ""

	var p atomic.Pointer[tea.Program]
	send := func(msg tea.Msg) {
		if prog := p.Load(); prog != nil {
			prog.Send(msg)
		}
	}

	// We wrap the WebRTC connection logic in a function so it can be called
	// after the user submits the form (if the form is shown).
	startCall := func(res tui.JoinResult) *tui.CallModel {
		log.Printf("Connecting to %s, Room: %s", res.ServerURL, res.RoomID)

		sigClient, err := signaling.NewClient(ctx, res.ServerURL, res.RoomID, res.Username)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[FATAL] Failed to connect to signaling server: %v\n", err)
			os.Exit(1)
		}
		// Notice: we can't easily defer sigClient.Close() here because we are in a closure.
		// Instead, we let the context cancellation handle it when main exits.
		go func() {
			<-ctx.Done()
			sigClient.Close()
		}()

		mesh := rtc.NewMeshManager(sigClient)

		cam := capture.NewCamera(15)
		camChan, err := cam.Start(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[FATAL] Failed to start camera: %v\nCheck if your camera is connected and if Windows Privacy Settings allow camera access.\n", err)
			os.Exit(1)
		}

		mic := capture.NewMicrophone()
		micTrack, err := mic.Start(ctx)
		if err != nil {
			log.Printf("Warning: failed to start microphone: %v", err)
		} else {
			mesh.SetLocalAudioTrack(micTrack)
			go func() {
				<-ctx.Done()
				mic.Close()
			}()
		}

		callModel := tui.NewCallModel(res.RoomID, mesh, cam, mic)

		mesh.OnPeerJoin = func(peerID, username string) {
			send(tui.PeerJoinMsg{PeerID: peerID, Username: username})
		}

		mesh.OnPeerLeave = func(peerID string) {
			send(tui.PeerLeaveMsg{PeerID: peerID})
		}

		mesh.OnFrame = func(peerID string, frame string) {
			send(tui.PeerFrameMsg{PeerID: peerID, Frame: frame})
		}

		// Capture frames from camera
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case img, ok := <-camChan:
					if !ok {
						return
					}
					send(tui.LocalFrameMsg{RawImage: img})
				}
			}
		}()

		mesh.Start()
		go func() {
			<-ctx.Done()
			mesh.Stop()
		}()

		return callModel
	}

	app := tui.NewAppModel(skipForm, *roomID, *username, *serverURL, startCall)
	prog := tea.NewProgram(app, tea.WithAltScreen())
	p.Store(prog)

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}

	log.Println("Shutting down...")
}
