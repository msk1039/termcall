package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/msk1039/termcall/internal/capture"
	"github.com/msk1039/termcall/internal/rtc"
	"github.com/msk1039/termcall/internal/signaling"
	"github.com/msk1039/termcall/internal/tui"
)

func main() {
	username := flag.String("username", "", "username for the call (with -room, skips the join form)")
	roomID := flag.String("room", "", "room ID to join (with -username, skips the join form)")
	serverURL := flag.String("server", "", "signaling server URL (defaults to "+tui.DefaultServerURL+")")
	allowInsecureWS := flag.Bool("allow-insecure-ws", false, "allow ws:// for a non-loopback development server")
	allowPrivateICE := flag.Bool("allow-private-ice", false, "allow private-network ICE candidates for a trusted LAN call")
	debugLog := flag.String("debug-log", "", "write diagnostic logs to this file (disabled by default)")
	flag.Usage = usage
	flag.Parse()

	log.SetOutput(io.Discard)
	if *debugLog != "" {
		file, err := tea.LogToFile(*debugLog, "debug")
		if err != nil {
			fmt.Fprintf(os.Stderr, "open debug log: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
	}

	skipForm, effectiveServer, err := invocationMode(*username, *roomID, *serverURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var cleanupMu sync.Mutex
	var cleanup func()
	defer func() {
		cancel()
		cleanupMu.Lock()
		defer cleanupMu.Unlock()
		if cleanup != nil {
			cleanup()
		}
	}()

	var programPointer atomic.Pointer[tea.Program]
	send := func(message tea.Msg) {
		if program := programPointer.Load(); program != nil {
			program.Send(message)
		}
	}

	startCall := func(result tui.JoinResult) (*tui.CallModel, error) {
		signalingClient, admission, err := signaling.Dial(ctx, signaling.DialOptions{
			URL:             result.ServerURL,
			RoomID:          result.RoomID,
			Username:        result.Username,
			AllowInsecureWS: *allowInsecureWS,
		})
		if err != nil {
			return nil, fmt.Errorf("connect to signaling server: %w", err)
		}

		mesh, err := rtc.NewMeshManagerWithOptions(ctx, signalingClient, admission, rtc.MeshOptions{
			AllowPrivateCandidates: *allowPrivateICE,
		})
		if err != nil {
			_ = signalingClient.Close()
			return nil, err
		}
		camera := capture.NewCamera(15)
		microphone := capture.NewMicrophone()
		callModel := tui.NewCallModel(
			ctx,
			admission.RoomID,
			mesh,
			camera,
			microphone,
		)

		mesh.OnPeerJoin = func(peerID, peerUsername string) {
			send(tui.PeerJoinMsg{PeerID: peerID, Username: peerUsername})
		}
		mesh.OnPeerLeave = func(peerID string) {
			send(tui.PeerLeaveMsg{PeerID: peerID})
		}
		mesh.OnFrame = func(peerID, frame string) {
			send(tui.PeerFrameMsg{PeerID: peerID, Frame: frame})
		}
		mesh.OnError = func(callErr error) {
			send(tui.CallErrorMsg{Err: callErr})
		}

		cleanupMu.Lock()
		cleanup = func() {
			camera.Close()
			microphone.Close()
			mesh.Stop()
		}
		cleanupMu.Unlock()
		return callModel, nil
	}

	app := tui.NewAppModel(skipForm, *roomID, *username, effectiveServer, startCall)
	program := tea.NewProgram(app, tea.WithAltScreen())
	programPointer.Store(program)
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "run TermCall: %v\n", err)
		os.Exit(1)
	}
}

func invocationMode(username, roomID, serverURL string) (bool, string, error) {
	if username == "" && roomID == "" && serverURL == "" {
		return false, "", nil
	}
	if username == "" || roomID == "" {
		return false, "", errors.New("-username and -room must be provided together; -server is optional")
	}
	if serverURL == "" {
		serverURL = tui.DefaultServerURL
	}
	return true, serverURL, nil
}

func usage() {
	output := flag.CommandLine.Output()
	fmt.Fprintln(output, "Usage:")
	fmt.Fprintln(output, "  termcall")
	fmt.Fprintln(output, "  termcall -username <name> -room <room> [-server <wss-url>]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Camera and microphone start disabled. ws:// is accepted only for")
	fmt.Fprintln(output, "loopback addresses unless -allow-insecure-ws is explicitly supplied.")
	fmt.Fprintln(output, "Private ICE candidates are blocked unless -allow-private-ice is supplied")
	fmt.Fprintln(output, "for a trusted LAN call.")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Flags:")
	flag.PrintDefaults()
}
