package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/msk1039/termcall/internal/ascii"
	"github.com/msk1039/termcall/internal/capture"
	"github.com/msk1039/termcall/internal/rtc"
)

const (
	// maxSendWidth and maxSendHeight cap the rendered frame dimensions sent
	// over the network. Frames larger than this waste bandwidth because
	// receivers display them in smaller grid cells.
	maxSendWidth  = 100
	maxSendHeight = 30
)

type CallModel struct {
	mesh   *rtc.MeshManager
	camera *capture.Camera
	mic    *capture.Microphone
	camOn  bool
	micOn  bool

	width  int
	height int

	peers        []string
	peerCells    map[string][][]ascii.Cell
	peerLegacy   map[string]string
	peerModes    map[string]ascii.RenderMode
	peerNames    map[string]string
	peerStats    map[string]rtc.PeerStats
	peerVolumes  map[string]float64
	removedPeers map[string]bool
	peerDecoders map[string]*ascii.DeltaDecoder
	localVolume  float64

	localCells [][]ascii.Cell
	renderer   *ascii.ColorRenderer
	showStats  bool
	roomID     string
}

func NewCallModel(roomID string, mesh *rtc.MeshManager, camera *capture.Camera, mic *capture.Microphone) *CallModel {
	return &CallModel{
		roomID:       roomID,
		mesh:         mesh,
		camera:       camera,
		mic:          mic,
		camOn:        true,
		micOn:        true,
		peerCells:    make(map[string][][]ascii.Cell),
		peerLegacy:   make(map[string]string),
		peerModes:    make(map[string]ascii.RenderMode),
		peerNames:    make(map[string]string),
		peerStats:    make(map[string]rtc.PeerStats),
		peerVolumes:  make(map[string]float64),
		removedPeers: make(map[string]bool),
		peerDecoders: make(map[string]*ascii.DeltaDecoder),
		renderer:     ascii.NewColorRenderer(ascii.ModeHalfBlock),
	}
}

func tickStats() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return StatsTickMsg{}
	})
}

func tickVolume() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return VolumeTickMsg{}
	})
}

func (m *CallModel) Init() tea.Cmd {
	return tea.Batch(tickStats(), tickVolume())
}

func (m *CallModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "v":
			m.camOn = !m.camOn
			if !m.camOn {
				m.mesh.BroadcastFrame([]byte("Camera Off"))
			}
			return m, nil
		case "m":
			m.micOn = !m.micOn
			m.mesh.SetMuteAudio(!m.micOn)
			return m, nil
		case "s":
			m.showStats = !m.showStats
			return m, nil
		case "n":
			current := m.renderer.GetMode()
			next := (int(current) + 1) % 3
			m.renderer.SetMode(ascii.RenderMode(next))
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case PeerJoinMsg:
		m.AddPeer(msg.PeerID, msg.Username)
		return m, nil
	case PeerLeaveMsg:
		m.RemovePeer(msg.PeerID)
		return m, nil
	case PeerFrameMsg:
		if m.removedPeers[msg.PeerID] {
			return m, nil
		}
		if _, ok := m.peerNames[msg.PeerID]; !ok {
			m.peerNames[msg.PeerID] = "Peer " + msg.PeerID
			m.peers = append(m.peers, msg.PeerID)
		}
		dec, ok := m.peerDecoders[msg.PeerID]
		if !ok {
			dec = ascii.NewDeltaDecoder()
			m.peerDecoders[msg.PeerID] = dec
		}
		cells, mode, isGrid := dec.DecodeCells(msg.Frame)
		if isGrid {
			m.peerCells[msg.PeerID] = cells
			m.peerModes[msg.PeerID] = mode
		} else {
			if len(msg.Frame) > 0 && (msg.Frame[0] == 'K' || msg.Frame[0] == 'D') {
				// failed decode, do nothing
			} else {
				m.peerLegacy[msg.PeerID] = msg.Frame
			}
		}
		return m, nil
	case LocalFrameMsg:
		if m.camOn {
			cells, sendStr := m.renderer.ConvertCells(msg.RawImage, maxSendWidth, maxSendHeight)
			m.localCells = cells
			m.mesh.BroadcastFrame([]byte(sendStr))
		} else {
			m.localCells = nil
		}
		return m, nil
	case StatsTickMsg:
		m.mesh.RLockPeers(func(peers map[string]*rtc.RemotePeer) {
			for pid, p := range peers {
				p.UpdateStats(2.0)
				m.peerStats[pid] = p.Stats
			}
		})
		return m, tickStats()
	case VolumeTickMsg:
		if m.micOn {
			// Mocking local volume pseudo-randomly for visual feedback
			// since intercepting hardware audio streams cleanly is complex via CGO.
			m.localVolume = 0.05 + rand.Float64()*0.2
		} else {
			m.localVolume = 0.0
		}

		m.mesh.RLockPeers(func(peers map[string]*rtc.RemotePeer) {
			for pid, p := range peers {
				if p.Player != nil {
					m.peerVolumes[pid] = p.Player.LastRMS()
				}
			}
		})
		return m, tickVolume()
	}
	return m, nil
}

func (m *CallModel) View() string {
	theme := GetCurrentTheme()

	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Calculate overall ping and loss for the top bar
	var maxPing float64 = 0
	var totalLoss uint32 = 0
	var localOutgoingKBps float64 = 0
	var localAudioOutKBps float64 = 0

	for _, pid := range m.peers {
		stats := m.peerStats[pid]
		if stats.RTTMs > maxPing {
			maxPing = stats.RTTMs
		}
		totalLoss += stats.PacketsLost
		// Approximate total outgoing as max of any peer's outgoing (since it's a broadcast)
		if stats.OutgoingKBps > localOutgoingKBps {
			localOutgoingKBps = stats.OutgoingKBps
			localAudioOutKBps = stats.AudioOutKBps
		}
	}

	titleText := lipgloss.NewStyle().Bold(true).Foreground(theme.TitleFg).Background(theme.TitleBg).Padding(0, 1).Render(fmt.Sprintf(" TermCall | Room: %s ", m.roomID))
	titleEnd := lipgloss.NewStyle().Foreground(theme.TitleBg).Background(theme.ControlBarBg).Render("")
	titlePart := lipgloss.JoinHorizontal(lipgloss.Top, titleText, titleEnd)

	var statsPart string
	if m.showStats && len(m.peers) > 0 {
		statsStart := lipgloss.NewStyle().Foreground(theme.StatsBg).Background(theme.ControlBarBg).Render("")
		statsText := lipgloss.NewStyle().Bold(true).Foreground(theme.StatusFg).Background(theme.StatsBg).Padding(0, 1).Render(fmt.Sprintf("Loss: %d | Ping: %.0f ms", totalLoss, maxPing))
		statsPart = lipgloss.JoinHorizontal(lipgloss.Top, statsStart, statsText)
	}

	spacerW := m.width - lipgloss.Width(titlePart) - lipgloss.Width(statsPart)
	if spacerW < 0 {
		spacerW = 0
	}
	spacer := lipgloss.NewStyle().Width(spacerW).Background(theme.ControlBarBg).Render("")

	titleBar := lipgloss.JoinHorizontal(lipgloss.Top, titlePart, spacer, statsPart)

	modeStr := "ASCII"
	if m.renderer.GetMode() == ascii.ModeColor256 {
		modeStr = "Color256"
	} else if m.renderer.GetMode() == ascii.ModeHalfBlock {
		modeStr = "HalfBlock"
	}

	controlBar := renderControls(m.width, m.camOn, m.micOn, m.showStats, modeStr, theme)
	controlH := lipgloss.Height(controlBar)

	availH := m.height - lipgloss.Height(titleBar) - controlH
	if availH < 0 {
		availH = 0
	}

	totalPeers := len(m.peers) + 1
	cols, boxW, boxH, innerW, innerH := computeGrid(totalPeers, m.width, availH)

	var cells []string

	// Local cell
	localFrameStr := ""
	if !m.camOn {
		localFrameStr = "Camera Off"
	} else if m.localCells != nil {
		upscaled := ascii.UpscaleCells(m.localCells, innerW, innerH)
		localFrameStr = ascii.SerialiseMode(upscaled, m.renderer.GetMode())
	} else {
		localFrameStr = "Waiting for camera..."
	}
	localName := "You (Local)" + renderVoiceActivity(m.localVolume, theme)

	var localStatsStr string
	if m.showStats {
		mockLocalStats := rtc.PeerStats{OutgoingKBps: localOutgoingKBps, AudioOutKBps: localAudioOutKBps}
		localStatsStr = renderOutgoingStats(mockLocalStats, theme)
	}
	cells = append(cells, renderCellTmux(localName, localFrameStr, localStatsStr, boxW, boxH, innerW, innerH, theme))

	// Remote cells
	for _, p := range m.peers {
		name := m.peerNames[p] + renderVoiceActivity(m.peerVolumes[p], theme)
		var frame string
		if cells, ok := m.peerCells[p]; ok && cells != nil {
			mode := m.peerModes[p]
			upscaled := ascii.UpscaleCells(cells, innerW, innerH)
			frame = ascii.SerialiseMode(upscaled, mode)
		} else if legacy, ok := m.peerLegacy[p]; ok {
			frame = legacy
		} else {
			frame = "Waiting for video..."
		}

		var remoteStatsStr string
		if m.showStats {
			remoteStatsStr = renderIncomingStats(m.peerStats[p], theme)
		}

		cells = append(cells, renderCellTmux(name, frame, remoteStatsStr, boxW, boxH, innerW, innerH, theme))
	}

	var rows []string
	for i := 0; i < len(cells); i += cols {
		end := i + cols
		if end > len(cells) {
			end = len(cells)
		}
		rowStr := lipgloss.JoinHorizontal(lipgloss.Top, cells[i:end]...)
		rows = append(rows, rowStr)
	}

	gridStr := lipgloss.JoinVertical(lipgloss.Center, rows...)
	gridArea := lipgloss.Place(m.width, availH, lipgloss.Center, lipgloss.Center, gridStr)

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, gridArea, controlBar)
}

func renderCellTmux(name, frame, statsStr string, boxW, boxH, maxInnerW, maxInnerH int, theme Theme) string {
	nameBg := theme.ActiveBtnBg
	nameFg := theme.ActiveBtnFg

	nameLabel := lipgloss.NewStyle().Background(nameBg).Foreground(nameFg).Bold(true).PaddingLeft(1).Render(name)
	nameArrow := lipgloss.NewStyle().Background(theme.BorderColor).Foreground(nameBg).Render("")

	topBarLeft := lipgloss.JoinHorizontal(lipgloss.Top, nameLabel, nameArrow)

	topBarRight := ""
	if statsStr != "" {
		statsArrow := lipgloss.NewStyle().Background(theme.BorderColor).Foreground(theme.StatsBg).Render("")
		topBarRight = lipgloss.JoinHorizontal(lipgloss.Top, statsArrow, statsStr)
	}

	fillerW := maxInnerW - lipgloss.Width(topBarLeft) - lipgloss.Width(topBarRight)
	if fillerW < 0 {
		fillerW = 0
	}
	filler := lipgloss.NewStyle().Background(theme.BorderColor).Width(fillerW).Render("")

	topBar := lipgloss.JoinHorizontal(lipgloss.Top, topBarLeft, filler, topBarRight)

	lines := strings.Split(frame, "\n")
	if len(lines) > maxInnerH {
		lines = lines[:maxInnerH]
	}
	safeFrame := strings.Join(lines, "\n")

	content := lipgloss.JoinVertical(lipgloss.Left, topBar, safeFrame)
	return lipgloss.Place(boxW, boxH, lipgloss.Center, lipgloss.Center, content)
}

func (m *CallModel) AddPeer(peerID, username string) {
	if _, ok := m.peerNames[peerID]; !ok {
		m.peers = append(m.peers, peerID)
	}
	m.peerNames[peerID] = username
}

func (m *CallModel) RemovePeer(peerID string) {
	for i, p := range m.peers {
		if p == peerID {
			m.peers = append(m.peers[:i], m.peers[i+1:]...)
			break
		}
	}
	delete(m.peerNames, peerID)
	delete(m.peerCells, peerID)
	delete(m.peerLegacy, peerID)
	delete(m.peerModes, peerID)
	delete(m.peerStats, peerID)
	delete(m.peerVolumes, peerID)
	delete(m.peerDecoders, peerID)
	m.removedPeers[peerID] = true
}
