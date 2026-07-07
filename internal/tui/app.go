package tui

import (
	"fmt"
	"image"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenHome screen = iota
	screenConnecting
	screenCall
)

type PeerJoinMsg struct {
	PeerID   string
	Username string
}

type PeerLeaveMsg struct {
	PeerID string
}

type PeerFrameMsg struct {
	PeerID string
	Frame  string
}

type LocalFrameMsg struct {
	RawImage image.Image
}

type StatsTickMsg struct{}

type VolumeTickMsg struct{}

// ConnectTickMsg drives the connecting-screen spinner animation.
type ConnectTickMsg time.Time

// CallReadyMsg is emitted when the asynchronous startCall completes, carrying
// either the ready call model or the error that aborted it.
type CallReadyMsg struct {
	callModel *CallModel
	err       error
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func connectTick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return ConnectTickMsg(t)
	})
}

type AppModel struct {
	currentScreen screen
	homeModel     *HomeModel
	callModel     *CallModel

	// Connecting-screen state
	connectingLabel string
	connectError    string
	spinnerFrame    int

	width  int
	height int
}

func NewAppModel(skipForm bool, defaultRoom, defaultUser, defaultServer string, startCall func(JoinResult) (*CallModel, error)) *AppModel {
	m := &AppModel{}

	if skipForm {
		// Flag path: connect synchronously before the UI comes up. Errors here
		// can't be shown in the TUI yet, so they're fatal.
		cm, err := startCall(JoinResult{RoomID: defaultRoom, Username: defaultUser, ServerURL: defaultServer})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[FATAL] %v\n", err)
			os.Exit(1)
		}
		m.currentScreen = screenCall
		m.callModel = cm
	} else {
		m.currentScreen = screenHome
		m.homeModel = NewHomeModel(func(res JoinResult) tea.Cmd {
			// Switch to the connecting screen immediately and run startCall in
			// a command (goroutine) so the UI keeps rendering a spinner instead
			// of freezing while the signaling/camera setup blocks.
			m.currentScreen = screenConnecting
			m.connectingLabel = fmt.Sprintf("%s @ %s", res.Username, res.RoomID)
			m.connectError = ""
			m.spinnerFrame = 0
			start := func() tea.Msg {
				cm, err := startCall(res)
				return CallReadyMsg{callModel: cm, err: err}
			}
			return tea.Batch(start, connectTick())
		})
	}

	return m
}

func (m *AppModel) Init() tea.Cmd {
	if m.currentScreen == screenHome {
		return m.homeModel.Init()
	}
	if m.currentScreen == screenCall && m.callModel != nil {
		return m.callModel.Init()
	}
	return nil
}

func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ConnectTickMsg:
		if m.currentScreen == screenConnecting && m.connectError == "" {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, connectTick()
		}
		return m, nil

	case CallReadyMsg:
		if msg.err != nil {
			m.connectError = msg.err.Error()
			return m, nil
		}
		m.currentScreen = screenCall
		m.callModel = msg.callModel
		if m.width > 0 && m.height > 0 {
			m.callModel.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		}
		return m, m.callModel.Init()

	case tea.KeyMsg:
		// Allow quitting from the connecting screen — both while the spinner is
		// active (cancel a slow/stuck connection) and on the error screen.
		if m.currentScreen == screenConnecting {
			switch msg.String() {
			case "q", "ctrl+c", "esc":
				return m, tea.Quit
			case "enter":
				if m.connectError != "" {
					return m, tea.Quit
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.homeModel != nil {
			m.homeModel.Update(msg)
		}
		if m.callModel != nil {
			m.callModel.Update(msg)
		}
		return m, nil

	case PeerFrameMsg, LocalFrameMsg, PeerJoinMsg, PeerLeaveMsg, StatsTickMsg, VolumeTickMsg:
		if m.currentScreen == screenCall && m.callModel != nil {
			cm, cmd := m.callModel.Update(msg)
			m.callModel = cm.(*CallModel)
			return m, cmd
		}
		return m, nil
	}

	if m.currentScreen == screenHome {
		hm, cmd := m.homeModel.Update(msg)
		// If the form just completed, onReady moved us off screenHome. Drop the
		// home model so later messages (e.g. a resize) don't re-enter the
		// completed form and strand us back on the connecting screen.
		if m.currentScreen == screenHome {
			m.homeModel = hm.(*HomeModel)
		} else {
			m.homeModel = nil
		}
		return m, cmd
	}
	if m.currentScreen == screenConnecting {
		return m, nil // absorb other input while connecting
	}
	if m.callModel != nil {
		cm, cmd := m.callModel.Update(msg)
		m.callModel = cm.(*CallModel)
		return m, cmd
	}
	return m, nil
}

func (m *AppModel) View() string {
	switch m.currentScreen {
	case screenHome:
		return m.homeModel.View()
	case screenConnecting:
		return m.renderConnecting()
	case screenCall:
		if m.callModel != nil {
			return m.callModel.View()
		}
	}
	return ""
}

func (m *AppModel) renderConnecting() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	theme := GetCurrentTheme()

	var content string
	if m.connectError != "" {
		content = fmt.Sprintf("✗  Could not connect\n\n  %s\n\n  press q to quit", m.connectError)
	} else {
		spinner := spinnerFrames[m.spinnerFrame]
		content = fmt.Sprintf("%s  Connecting to room %s…", spinner, m.connectingLabel)
	}

	box := lipgloss.NewStyle().
		Foreground(theme.ControlBarFg).
		Background(theme.ControlBarBg).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
