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
	username := flag.String("username", "", "Username for the call (with -room, skips the join form)")
	roomID := flag.String("room", "", "Room ID to join (with -username, skips the join form)")
	serverURL := flag.String("server", "", "Signaling server URL (defaults to "+tui.DefaultServerURL+")")
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  termcall                                         Start the TUI join form.")
		fmt.Fprintln(out, "  termcall -username <name> -room <room> [-server <url>]")
		fmt.Fprintln(out, "                                                   Skip the form and join directly.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "With flags, both -username and -room are required; -server is optional and")
		fmt.Fprintln(out, "defaults to "+tui.DefaultServerURL+".")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Setup context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		log.Fatalf("fatal: %v", err)
	}
	defer f.Close()

	// Two ways in: no flags -> TUI join form; or -username + -room (with
	// -server optional). Anything in between is a partial invocation.
	skipForm := false
	effectiveServer := *serverURL
	switch {
	case *username == "" && *roomID == "" && *serverURL == "":
		// No flags: show the join form.
	case *username != "" && *roomID != "":
		skipForm = true
		if effectiveServer == "" {
			effectiveServer = tui.DefaultServerURL
		}
	default:
		fmt.Fprintln(os.Stderr, "please pass all the arguments: -username and -room are required (-server is optional).")
		flag.Usage()
		os.Exit(2)
	}

	var p atomic.Pointer[tea.Program]
	send := func(msg tea.Msg) {
		if prog := p.Load(); prog != nil {
			prog.Send(msg)
		}
	}

	// We wrap the WebRTC connection logic in a function so it can be called
	// after the user submits the form (if the form is shown).
	startCall := func(res tui.JoinResult) (*tui.CallModel, error) {
		log.Printf("Connecting to %s, Room: %s", res.ServerURL, res.RoomID)

		sigClient, err := signaling.NewClient(ctx, res.ServerURL, res.RoomID, res.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to signaling server: %w", err)
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
			return nil, fmt.Errorf("failed to start camera: %v (check the camera is connected and OS privacy settings allow access)", err)
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

		return callModel, nil
	}

	app := tui.NewAppModel(skipForm, *roomID, *username, effectiveServer, startCall)
	prog := tea.NewProgram(app, tea.WithAltScreen())
	p.Store(prog)

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}

	log.Println("Shutting down...")
}
