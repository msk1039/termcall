package tui

import (
	"image"

	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	screenHome screen = iota
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

type AppModel struct {
	currentScreen screen
	homeModel     *HomeModel
	callModel     *CallModel

	width  int
	height int
}

func NewAppModel(skipForm bool, defaultRoom, defaultUser, defaultServer string, startCall func(JoinResult) *CallModel) *AppModel {
	m := &AppModel{}

	if skipForm {
		m.currentScreen = screenCall
		m.callModel = startCall(JoinResult{RoomID: defaultRoom, Username: defaultUser, ServerURL: defaultServer})
	} else {
		m.currentScreen = screenHome
		m.homeModel = NewHomeModel(func(res JoinResult) tea.Cmd {
			m.currentScreen = screenCall
			m.callModel = startCall(res)
			
			if m.width > 0 && m.height > 0 {
				m.callModel.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			}
			return m.callModel.Init()
		})
	}

	return m
}

func (m *AppModel) Init() tea.Cmd {
	if m.currentScreen == screenHome {
		return m.homeModel.Init()
	}
	return m.callModel.Init()
}

func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "t" && m.currentScreen == screenCall {
			NextTheme()
			return m, nil
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
		m.homeModel = hm.(*HomeModel)
		return m, cmd
	}

	cm, cmd := m.callModel.Update(msg)
	m.callModel = cm.(*CallModel)
	return m, cmd
}

func (m *AppModel) View() string {
	if m.currentScreen == screenHome {
		return m.homeModel.View()
	}
	if m.callModel != nil {
		return m.callModel.View()
	}
	return ""
}
