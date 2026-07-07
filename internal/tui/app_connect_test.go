package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestConnectingQuit(t *testing.T) {
	m := &AppModel{currentScreen: screenConnecting, width: 80, height: 24}

	for _, k := range []string{"q", "ctrl+c", "esc"} {
		_, cmd := m.Update(keyMsg(k))
		if cmd == nil {
			t.Errorf("%q during connecting should quit (non-nil cmd)", k)
		}
	}

	// Random key is absorbed (no quit).
	if _, cmd := m.Update(keyMsg("x")); cmd != nil {
		t.Error("random key during connecting should be absorbed")
	}

	// enter on the spinner (no error) must NOT quit.
	if _, cmd := m.Update(keyMsg("enter")); cmd != nil {
		t.Error("enter during spinner should not quit")
	}

	// enter on the error screen quits.
	m.connectError = "boom"
	if _, cmd := m.Update(keyMsg("enter")); cmd == nil {
		t.Error("enter on error screen should quit")
	}
}

func TestResizeAfterFormSafe(t *testing.T) {
	// After the form completes the home model is nil; a resize must not
	// re-enter any completed form or change the screen.
	m := &AppModel{currentScreen: screenCall, width: 80, height: 24}
	mm, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	am := mm.(*AppModel)
	if cmd != nil {
		t.Error("resize should not produce a cmd")
	}
	if am.currentScreen != screenCall {
		t.Errorf("screen changed to %v", am.currentScreen)
	}
	if am.width != 100 || am.height != 40 {
		t.Error("size not updated")
	}
}
