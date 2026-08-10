package tui

import (
	"context"
	"fmt"
	"image"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/msk1039/termcall/internal/ascii"
	"github.com/msk1039/termcall/internal/capture"
	"github.com/msk1039/termcall/internal/rtc"
	"github.com/pion/webrtc/v4"
)

const (
	maxSendWidth  = ascii.MaxFrameWidth
	maxSendHeight = ascii.MaxFrameHeight
)

type CallModel struct {
	ctx    context.Context
	mesh   callMesh
	camera capture.FrameSource
	mic    microphoneCapture

	cameraFrames <-chan image.Image
	camOn        bool
	micOn        bool
	camStarting  bool
	micStarting  bool
	callError    string

	width  int
	height int

	peers        []string
	peerCells    map[string][][]ascii.Cell
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

type callMesh interface {
	Start()
	SetLocalAudioTrack(webrtc.TrackLocal)
	SetMuteAudio(bool)
	BroadcastFrame([]byte)
	RLockPeers(func(map[string]*rtc.RemotePeer))
}

type microphoneCapture interface {
	Start(context.Context) (webrtc.TrackLocal, error)
	LastRMS() float64
	Close()
}

func NewCallModel(
	ctx context.Context,
	roomID string,
	mesh callMesh,
	camera capture.FrameSource,
	mic microphoneCapture,
) *CallModel {
	return &CallModel{
		ctx:          ctx,
		roomID:       roomID,
		mesh:         mesh,
		camera:       camera,
		mic:          mic,
		peers:        []string{},
		peerCells:    map[string][][]ascii.Cell{},
		peerModes:    map[string]ascii.RenderMode{},
		peerNames:    map[string]string{},
		peerStats:    map[string]rtc.PeerStats{},
		peerVolumes:  map[string]float64{},
		removedPeers: map[string]bool{},
		peerDecoders: map[string]*ascii.DeltaDecoder{},
		localCells:   [][]ascii.Cell{},
		renderer:     ascii.NewColorRenderer(ascii.ModeHalfBlock),
	}
}

func tickStats() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return StatsTickMsg{}
	})
}

func tickVolume() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return VolumeTickMsg{}
	})
}

func (model *CallModel) Init() tea.Cmd {
	startMesh := func() tea.Msg {
		model.mesh.Start()
		return nil
	}
	return tea.Batch(startMesh, tickStats(), tickVolume())
}

func (model *CallModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "ctrl+c", "q":
			model.camera.Close()
			model.mic.Close()
			return model, tea.Quit
		case "v":
			return model.toggleCamera()
		case "m":
			return model.toggleMicrophone()
		case "s":
			model.showStats = !model.showStats
			return model, nil
		case "n":
			currentMode := model.renderer.GetMode()
			nextMode := (int(currentMode) + 1) % 3
			model.renderer.SetMode(ascii.RenderMode(nextMode))
			return model, nil
		}
	case tea.WindowSizeMsg:
		model.width = message.Width
		model.height = message.Height
	case PeerJoinMsg:
		model.AddPeer(message.PeerID, message.Username)
		return model, nil
	case PeerLeaveMsg:
		model.RemovePeer(message.PeerID)
		return model, nil
	case PeerFrameMsg:
		model.handlePeerFrame(message)
		return model, nil
	case CameraStartedMsg:
		model.camStarting = false
		if message.Err != nil {
			model.setError(fmt.Errorf("camera: %w", message.Err))
			return model, nil
		}
		model.cameraFrames = message.Frames
		model.camOn = true
		model.renderer.Reset()
		model.callError = ""
		return model, waitForCameraFrame(message.Frames)
	case LocalFrameMsg:
		return model.handleLocalFrame(message)
	case MicrophoneStartedMsg:
		model.micStarting = false
		if message.Err != nil {
			model.setError(fmt.Errorf("microphone: %w", message.Err))
			return model, nil
		}
		model.mesh.SetLocalAudioTrack(message.Track)
		model.mesh.SetMuteAudio(false)
		model.micOn = true
		model.callError = ""
		return model, nil
	case CallErrorMsg:
		model.setError(message.Err)
		return model, nil
	case StatsTickMsg:
		model.mesh.RLockPeers(func(peers map[string]*rtc.RemotePeer) {
			for peerID, peer := range peers {
				peer.UpdateStats(2)
				model.peerStats[peerID] = peer.StatsSnapshot()
			}
		})
		return model, tickStats()
	case VolumeTickMsg:
		model.localVolume = 0
		model.mesh.RLockPeers(func(peers map[string]*rtc.RemotePeer) {
			for peerID, peer := range peers {
				if player := peer.Player(); player != nil {
					model.peerVolumes[peerID] = player.LastRMS()
				}
			}
		})
		return model, tickVolume()
	}
	return model, nil
}

func (model *CallModel) toggleCamera() (tea.Model, tea.Cmd) {
	if model.camStarting {
		return model, nil
	}
	if model.camOn {
		model.camera.Close()
		model.camOn = false
		model.cameraFrames = nil
		model.localCells = [][]ascii.Cell{}
		model.renderer.Reset()
		model.mesh.BroadcastFrame([]byte(ascii.StatusFrame("Camera off")))
		return model, nil
	}

	model.camStarting = true
	model.callError = ""
	return model, startCamera(model.ctx, model.camera)
}

func (model *CallModel) toggleMicrophone() (tea.Model, tea.Cmd) {
	if model.micStarting {
		return model, nil
	}
	if model.micOn {
		model.mesh.SetMuteAudio(true)
		model.mic.Close()
		model.micOn = false
		model.localVolume = 0
		return model, nil
	}

	model.micStarting = true
	model.callError = ""
	return model, startMicrophone(model.ctx, model.mic)
}

func (model *CallModel) handlePeerFrame(message PeerFrameMsg) {
	if model.removedPeers[message.PeerID] {
		return
	}
	if _, exists := model.peerNames[message.PeerID]; !exists {
		model.peerNames[message.PeerID] = "Peer " + message.PeerID
		model.peers = append(model.peers, message.PeerID)
	}
	if message.Frame == ascii.StatusFrame("Camera off") {
		delete(model.peerCells, message.PeerID)
		delete(model.peerModes, message.PeerID)
		return
	}
	decoder, exists := model.peerDecoders[message.PeerID]
	if !exists {
		decoder = ascii.NewDeltaDecoder()
		model.peerDecoders[message.PeerID] = decoder
	}
	cells, mode, err := decoder.DecodeCells(message.Frame)
	if err != nil {
		model.callError = "Dropped an invalid video frame from " + model.peerNames[message.PeerID]
		return
	}
	model.peerCells[message.PeerID] = cells
	model.peerModes[message.PeerID] = mode
}

func (model *CallModel) handleLocalFrame(message LocalFrameMsg) (tea.Model, tea.Cmd) {
	if message.Source != model.cameraFrames {
		return model, nil
	}
	if message.Closed {
		model.cameraFrames = nil
		if model.camOn {
			model.camOn = false
			model.callError = "Camera stream stopped"
		}
		return model, nil
	}
	if model.camOn && message.RawImage != nil {
		cells, encoded := model.renderer.ConvertCells(message.RawImage, maxSendWidth, maxSendHeight)
		if encoded != "" {
			model.localCells = cells
			model.mesh.BroadcastFrame([]byte(encoded))
		}
	}
	return model, waitForCameraFrame(model.cameraFrames)
}

func startCamera(ctx context.Context, camera capture.FrameSource) tea.Cmd {
	return func() tea.Msg {
		frames, err := camera.Start(ctx)
		return CameraStartedMsg{Frames: frames, Err: err}
	}
}

func waitForCameraFrame(frames <-chan image.Image) tea.Cmd {
	return func() tea.Msg {
		frame, ok := <-frames
		return LocalFrameMsg{
			RawImage: frame,
			Source:   frames,
			Closed:   !ok,
		}
	}
}

func startMicrophone(ctx context.Context, microphone microphoneCapture) tea.Cmd {
	return func() tea.Msg {
		track, err := microphone.Start(ctx)
		return MicrophoneStartedMsg{Track: track, Err: err}
	}
}

func (model *CallModel) setError(err error) {
	if err == nil {
		return
	}
	var builder strings.Builder
	for _, char := range err.Error() {
		if unicode.IsPrint(char) && !unicode.IsControl(char) {
			builder.WriteRune(char)
		}
		if builder.Len() >= 160 {
			break
		}
	}
	model.callError = builder.String()
}

func (model *CallModel) View() string {
	theme := GetCurrentTheme()
	if model.width == 0 || model.height == 0 {
		return ""
	}

	var maxPing float64
	var totalLoss uint32
	var localOutgoingKBps float64
	var localAudioOutKBps float64
	for _, peerID := range model.peers {
		stats := model.peerStats[peerID]
		if stats.RTTMs > maxPing {
			maxPing = stats.RTTMs
		}
		totalLoss += stats.PacketsLost
		if stats.OutgoingKBps > localOutgoingKBps {
			localOutgoingKBps = stats.OutgoingKBps
			localAudioOutKBps = stats.AudioOutKBps
		}
	}

	titleText := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TitleFg).
		Background(theme.TitleBg).
		Padding(0, 1).
		Render(fmt.Sprintf(" TermCall | Room: %s ", model.roomID))
	titleEnd := lipgloss.NewStyle().
		Foreground(theme.TitleBg).
		Background(theme.ControlBarBg).
		Render("")
	titlePart := lipgloss.JoinHorizontal(lipgloss.Top, titleText, titleEnd)

	var statsPart string
	if model.showStats && len(model.peers) > 0 {
		statsStart := lipgloss.NewStyle().
			Foreground(theme.StatsBg).
			Background(theme.ControlBarBg).
			Render("")
		statsText := lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.StatusFg).
			Background(theme.StatsBg).
			Padding(0, 1).
			Render(fmt.Sprintf("Loss: %d | Ping: %.0f ms", totalLoss, maxPing))
		statsPart = lipgloss.JoinHorizontal(lipgloss.Top, statsStart, statsText)
	}

	spacerWidth := model.width - lipgloss.Width(titlePart) - lipgloss.Width(statsPart)
	if spacerWidth < 0 {
		spacerWidth = 0
	}
	spacer := lipgloss.NewStyle().
		Width(spacerWidth).
		Background(theme.ControlBarBg).
		Render("")
	titleBar := lipgloss.JoinHorizontal(lipgloss.Top, titlePart, spacer, statsPart)

	statusBar := ""
	if model.callError != "" {
		statusBar = lipgloss.NewStyle().
			Width(model.width).
			Foreground(theme.InactiveBtnFg).
			Background(theme.InactiveBtnBg).
			Render(" " + ansi.Truncate(model.callError, max(1, model.width-2), "…"))
	}

	modeName := "ASCII"
	switch model.renderer.GetMode() {
	case ascii.ModeColor256:
		modeName = "Color256"
	case ascii.ModeHalfBlock:
		modeName = "HalfBlock"
	}
	controlBar := renderControls(
		model.width,
		model.camOn,
		model.micOn,
		model.showStats,
		modeName,
		theme,
	)

	availableHeight := model.height - lipgloss.Height(titleBar) - lipgloss.Height(statusBar) - lipgloss.Height(controlBar)
	if availableHeight < 0 {
		availableHeight = 0
	}
	columns, boxWidth, boxHeight, innerWidth, innerHeight := computeGrid(
		len(model.peers)+1,
		model.width,
		availableHeight,
	)

	videoCells := make([]string, 0, len(model.peers)+1)
	localFrame := "Camera off"
	showCameraOffPlaceholder := !model.camOn && !model.camStarting
	if model.camStarting {
		localFrame = "Starting camera..."
	} else if model.camOn && len(model.localCells) > 0 {
		localFrame = ascii.SerialiseMode(model.localCells, model.renderer.GetMode())
	} else if model.camOn {
		localFrame = "Waiting for camera..."
	}
	localName := "You (Local)" + renderVoiceActivity(model.localVolume, theme)
	var localStats string
	if model.showStats {
		localStats = renderOutgoingStats(rtc.PeerStats{
			OutgoingKBps: localOutgoingKBps,
			AudioOutKBps: localAudioOutKBps,
		}, theme)
	}
	videoCells = append(videoCells, renderCellTmux(videoCellOptions{
		name:                     localName,
		frame:                    localFrame,
		showCameraOffPlaceholder: showCameraOffPlaceholder,
		stats:                    localStats,
		boxWidth:                 boxWidth,
		boxHeight:                boxHeight,
		maxInnerWidth:            innerWidth,
		maxInnerHeight:           innerHeight,
		theme:                    theme,
	}))

	for _, peerID := range model.peers {
		name := model.peerNames[peerID] + renderVoiceActivity(model.peerVolumes[peerID], theme)
		frame := "Camera off"
		showCameraOffPlaceholder := true
		if cells := model.peerCells[peerID]; len(cells) > 0 {
			frame = ascii.SerialiseMode(cells, model.peerModes[peerID])
			showCameraOffPlaceholder = false
		}
		var remoteStats string
		if model.showStats {
			remoteStats = renderIncomingStats(model.peerStats[peerID], theme)
		}
		videoCells = append(videoCells, renderCellTmux(videoCellOptions{
			name:                     name,
			frame:                    frame,
			showCameraOffPlaceholder: showCameraOffPlaceholder,
			stats:                    remoteStats,
			boxWidth:                 boxWidth,
			boxHeight:                boxHeight,
			maxInnerWidth:            innerWidth,
			maxInnerHeight:           innerHeight,
			theme:                    theme,
		}))
	}

	rows := make([]string, 0, (len(videoCells)+columns-1)/columns)
	for index := 0; index < len(videoCells); index += columns {
		end := min(index+columns, len(videoCells))
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, videoCells[index:end]...))
	}
	grid := lipgloss.JoinVertical(lipgloss.Center, rows...)
	gridArea := lipgloss.Place(model.width, availableHeight, lipgloss.Center, lipgloss.Center, grid)

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, statusBar, gridArea, controlBar)
}

type videoCellOptions struct {
	name                     string
	frame                    string
	showCameraOffPlaceholder bool
	stats                    string
	boxWidth                 int
	boxHeight                int
	maxInnerWidth            int
	maxInnerHeight           int
	theme                    Theme
}

func renderCellTmux(options videoCellOptions) string {
	safeFrame := options.frame
	if options.showCameraOffPlaceholder {
		safeFrame = cameraOffPlaceholder(
			options.maxInnerWidth,
			options.maxInnerHeight,
			options.theme,
		)
	} else {
		lines := strings.Split(options.frame, "\n")
		if len(lines) > options.maxInnerHeight {
			lines = lines[:options.maxInnerHeight]
		}
		for index, line := range lines {
			lines[index] = ansi.Truncate(line, options.maxInnerWidth, "")
		}
		safeFrame = strings.Join(lines, "\n")
	}

	name := ansi.Truncate(options.name, max(1, options.maxInnerWidth/2), "…")
	nameLabel := lipgloss.NewStyle().
		Background(options.theme.ActiveBtnBg).
		Foreground(options.theme.ActiveBtnFg).
		Bold(true).
		PaddingLeft(1).
		Render(name)
	nameArrow := lipgloss.NewStyle().
		Background(options.theme.BorderColor).
		Foreground(options.theme.ActiveBtnBg).
		Render("")
	topBarLeft := lipgloss.JoinHorizontal(lipgloss.Top, nameLabel, nameArrow)

	topBarRight := ""
	if options.stats != "" {
		stats := ansi.Truncate(options.stats, max(1, options.maxInnerWidth-lipgloss.Width(topBarLeft)-1), "…")
		statsArrow := lipgloss.NewStyle().
			Background(options.theme.BorderColor).
			Foreground(options.theme.StatsBg).
			Render("")
		topBarRight = lipgloss.JoinHorizontal(lipgloss.Top, statsArrow, stats)
	}

	fillerWidth := options.maxInnerWidth - lipgloss.Width(topBarLeft) - lipgloss.Width(topBarRight)
	if fillerWidth < 0 {
		fillerWidth = 0
	}
	filler := lipgloss.NewStyle().
		Background(options.theme.BorderColor).
		Width(fillerWidth).
		Render("")
	topBar := lipgloss.JoinHorizontal(lipgloss.Top, topBarLeft, filler, topBarRight)
	content := lipgloss.JoinVertical(lipgloss.Center, topBar, safeFrame)
	return lipgloss.Place(options.boxWidth, options.boxHeight, lipgloss.Center, lipgloss.Center, content)
}

func cameraOffPlaceholder(width, height int, theme Theme) string {
	if width < 1 || height < 1 {
		return ""
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(theme.CameraOffFg).
		Background(theme.CameraOffBg).
		Render("Camera off")
}

func (model *CallModel) AddPeer(peerID, username string) {
	if _, exists := model.peerNames[peerID]; !exists {
		model.peers = append(model.peers, peerID)
	}
	model.peerNames[peerID] = username
	delete(model.removedPeers, peerID)
}

func (model *CallModel) RemovePeer(peerID string) {
	for index, existingPeerID := range model.peers {
		if existingPeerID == peerID {
			model.peers = append(model.peers[:index], model.peers[index+1:]...)
			break
		}
	}
	delete(model.peerNames, peerID)
	delete(model.peerCells, peerID)
	delete(model.peerModes, peerID)
	delete(model.peerStats, peerID)
	delete(model.peerVolumes, peerID)
	delete(model.peerDecoders, peerID)
	model.removedPeers[peerID] = true
}
