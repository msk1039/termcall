package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type JoinResult struct {
	RoomID    string
	Username  string
	ServerURL string
}

type JoinModel struct {
	form    *huh.Form
	onReady func(JoinResult) tea.Cmd
}

func NewJoinModel(onReady func(JoinResult) tea.Cmd) *JoinModel {
	var roomID, username string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Room ID").
				Value(&roomID).
				Validate(func(s string) error {
					if len(s) == 0 {
						return errors.New("room ID cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Title("Username").
				Value(&username).
				Validate(func(s string) error {
					if len(s) == 0 {
						return errors.New("username cannot be empty")
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeDracula())

	form.Init()

	return &JoinModel{
		form:    form,
		onReady: onReady,
	}
}

func (m *JoinModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m *JoinModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	}

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f

		if m.form.State == huh.StateCompleted {
			res := JoinResult{
				RoomID:   m.form.GetString("Room ID"),
				Username: m.form.GetString("Username"),
			}
			return m, m.onReady(res)
		}
	}

	return m, cmd
}

func (m *JoinModel) View() string {
	theme := GetCurrentTheme()
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TitleFg).
		Background(theme.TitleBg).
		Padding(0, 1).
		Render(" TermCall - Join Room ")

	formView := m.form.View()

	container := lipgloss.NewStyle().
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderColor).
		Render(lipgloss.JoinVertical(lipgloss.Center, title, "\n", formView))

	return lipgloss.Place(
		100, // We will override this in the main View using actual width
		40,  // We will override this in the main View using actual height
		lipgloss.Center, lipgloss.Center,
		container,
	)
}
