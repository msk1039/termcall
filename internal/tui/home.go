package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/msk1039/termcall/internal/protocol"
)

// DefaultServerURL is the production signaling endpoint used when callers do
// not provide one explicitly. Non-local deployments must use TLS.
const DefaultServerURL = "wss://termcall-server.m4yank.com:8443/ws"

type HomeModel struct {
	form    *huh.Form
	onReady func(JoinResult) tea.Cmd
	width   int
	height  int
}

var formFieldKeys = []string{"Username", "Room ID", "Server URL"}

func (m *HomeModel) currentFieldIndex() int {
	key := m.form.GetFocusedField().GetKey()
	for i, formKey := range formFieldKeys {
		if formKey == key {
			return i
		}
	}
	return 0
}

func NewHomeModel(onReady func(JoinResult) tea.Cmd) *HomeModel {
	roomID := "new"
	var username string
	serverURL := DefaultServerURL

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("Username").
				Title("Username").
				Value(&username).
				Validate(requiredField("username")),
			huh.NewInput().
				Key("Room ID").
				Title("Room ID (use 'new' to create)").
				Value(&roomID).
				Validate(func(value string) error {
					if err := protocol.ValidateRoomID(value); err != nil {
						return errors.New("room ID must be 'new' or a room UUID")
					}
					return nil
				}),
			huh.NewInput().
				Key("Server URL").
				Title("Server URL").
				Value(&serverURL).
				Validate(requiredField("server URL")),
		).WithShowHelp(false),
	).WithTheme(huh.ThemeDracula()).WithWidth(50)

	form.Init()

	return &HomeModel{
		form:    form,
		onReady: onReady,
	}
}

func requiredField(name string) func(string) error {
	return func(value string) error {
		if value == "" {
			return errors.New(name + " cannot be empty")
		}
		return nil
	}
}

func (m *HomeModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m *HomeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up":
			if m.form.State == huh.StateNormal {
				return m, m.form.PrevField()
			}
		case "down":
			isNormal := m.form.State == huh.StateNormal
			hasNextField := m.currentFieldIndex() < len(formFieldKeys)-1
			if isNormal && hasNextField {
				return m, m.form.NextField()
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	formModel, cmd := m.form.Update(msg)
	form, ok := formModel.(*huh.Form)
	if !ok {
		return m, cmd
	}
	m.form = form

	if m.form.State != huh.StateCompleted {
		return m, cmd
	}

	result := JoinResult{
		Username:  m.form.GetString("Username"),
		RoomID:    m.form.GetString("Room ID"),
		ServerURL: m.form.GetString("Server URL"),
	}
	return m, m.onReady(result)
}

func (m *HomeModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	theme := GetCurrentTheme()
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TitleFg).
		Background(theme.TitleBg).
		Padding(0, 2).
		Render(" TermCall ")

	subtitle := lipgloss.NewStyle().
		Foreground(theme.StatusFg).
		Render("Private peer-to-peer calls in your terminal")

	controls := lipgloss.NewStyle().
		Foreground(theme.ControlBarFg).
		Render("↑/↓ switch  •  enter submit  •  ctrl+c quit")

	privacy := lipgloss.NewStyle().
		Foreground(theme.PeerLabelFg).
		Render("Camera and microphone start disabled")

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		"",
		subtitle,
		"",
		m.form.View(),
		"",
		privacy,
		controls,
	)
	panel := lipgloss.NewStyle().
		Padding(1, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderColor).
		Background(theme.ControlBarBg).
		Render(content)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}
