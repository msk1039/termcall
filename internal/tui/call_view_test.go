package tui

import (
	"context"
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/msk1039/termcall/internal/ascii"
)

func TestCameraOffPlaceholderFillsVideoArea(t *testing.T) {
	theme := GetCurrentTheme()
	placeholder := cameraOffPlaceholder(24, 6, theme)

	if width := lipgloss.Width(placeholder); width != 24 {
		t.Fatalf("placeholder width = %d, want 24", width)
	}
	if height := lipgloss.Height(placeholder); height != 6 {
		t.Fatalf("placeholder height = %d, want 6", height)
	}
	if !strings.Contains(placeholder, "Camera off") {
		t.Fatalf("placeholder = %q, want camera-off label", placeholder)
	}
	if theme.CameraOffBg != lipgloss.Color("#4a4a4a") {
		t.Fatalf("camera-off background = %q, want gray", theme.CameraOffBg)
	}
}

func TestRemoteCameraOffStatusUsesPlaceholder(t *testing.T) {
	model := NewCallModel(
		context.Background(),
		"room",
		&fakeCallMesh{},
		&fakeCamera{frames: make(chan image.Image)},
		&fakeMicrophone{},
	)
	model.AddPeer("peer", "Alice")
	model.peerCells["peer"] = [][]ascii.Cell{{{Char: 'x'}}}
	model.peerModes["peer"] = ascii.ModeASCII

	model.handlePeerFrame(PeerFrameMsg{
		PeerID: "peer",
		Frame:  ascii.StatusFrame("Camera off"),
	})

	if _, exists := model.peerCells["peer"]; exists {
		t.Fatal("camera-off status left a remote video frame behind")
	}
	if _, exists := model.peerModes["peer"]; exists {
		t.Fatal("camera-off status left a remote video mode behind")
	}
}
