package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/gdamore/tcell/v2"

	"github.com/msk1039/termcall/internal/globe"
)

// DefaultServerURL is the signaling server used when none is provided — both
// as the join form's default value and as the fallback for the
// -username/-room CLI shortcut.
const DefaultServerURL = "ws://13.127.137.230:8080/ws"

// cfg is the single place to tweak the home-screen globe. Edit the values and
// re-run. It mirrors the CONFIG block in reference/globe/cmd/globe/main.go.
var cfg = struct {
	// RotationSpeed is how fast the globe's own axis spins (units/frame, ×0.001).
	// 0 = static, 5 = gentle spin, 20 = fast.
	RotationSpeed float32
	// CamOrbitSpeed is how fast the camera orbits the globe (units/frame, ×0.001).
	CamOrbitSpeed float32
	// TiltX (camera beta) tilts the north pole toward/away from you.
	// TiltY (camera alpha) yaws the globe left/right.
	// TiltZ (texture roll) is the initial spin angle baked into the pose.
	TiltX, TiltY, TiltZ float32
	// Zoom is the camera distance from the globe center. Smaller = bigger globe.
	// No lower clamp, so you can go below 1.0 for dramatic close-ups.
	Zoom float32
	// CanvasScale enlarges the internal render buffer relative to the terminal's
	// smaller dimension. 1.0 = canvas fits (globe centered, sides filled with
	// the base background). >1.0 = canvas overflows and is clipped, letting the
	// globe grow beyond the smaller dimension (combine with lower Zoom).
	CanvasScale float32
	// OffsetX / OffsetY shift the globe from its centered position, in terminal
	// cells (columns / rows). 0/0 = centered. +OffsetX moves right, +OffsetY down.
	OffsetX int
	OffsetY int
	// FrameRate is the refresh rate in frames per second.
	FrameRate int
	// Night toggles day/night terminator shading + city lights.
	Night bool
	// Clouds enables the drifting cloud layer.
	Clouds bool
	// CloudSpeed is the cloud drift rate (units/frame, ×0.001), independent of
	// RotationSpeed so clouds lead/lag the surface for parallax.
	CloudSpeed float32
	// CloudOpacity scales day-side cloud brightness (1.0 = full bright white).
	// CloudNightOpacity scales night-side cloud brightness (0 = invisible).
	CloudOpacity      float32
	CloudNightOpacity float32
	// CloudCharBright overrides the ASCII char used for the brightest cloud
	// glyph (e.g. "w", "*", "#", "░", "·"). Empty = use the texture's own.
	CloudCharBright string
}{
	RotationSpeed:     0.5,
	CamOrbitSpeed:     1,
	TiltX:             0.6,
	TiltY:             -0.2,
	TiltZ:             0,
	Zoom:              1.4,
	CanvasScale:       1.9,
	OffsetX:           -10,
	OffsetY:           20,
	FrameRate:         30,
	Night:             true,
	Clouds:            true,
	CloudSpeed:        5,
	CloudOpacity:      1.4,
	CloudNightOpacity: 0.40,
	CloudCharBright:   "o",
}

type GlobeTickMsg time.Time

func tickGlobe() tea.Cmd {
	fps := cfg.FrameRate
	if fps < 1 {
		fps = 30
	}
	return tea.Tick(time.Second/time.Duration(fps), func(t time.Time) tea.Msg {
		return GlobeTickMsg(t)
	})
}

// base16 terminal palette (true-color hex values).
var base16 = []tcell.Color{
	tcell.NewRGBColor(0x10, 0x11, 0x13), // 0  base00
	tcell.NewRGBColor(0x16, 0x17, 0x19), // 1  base01
	tcell.NewRGBColor(0x1e, 0x1f, 0x22), // 2  base02
	tcell.NewRGBColor(0x55, 0x4a, 0x62), // 3  base03
	tcell.NewRGBColor(0x78, 0x74, 0x87), // 4  base04
	tcell.NewRGBColor(0x9f, 0x9e, 0x99), // 5  base05
	tcell.NewRGBColor(0xb5, 0xb3, 0xb0), // 6  base06
	tcell.NewRGBColor(0xe1, 0xff, 0xe5), // 7  base07
	tcell.NewRGBColor(0x6f, 0x2e, 0x2a), // 8  base08
	tcell.NewRGBColor(0xac, 0x7f, 0x7b), // 9  base09
	tcell.NewRGBColor(0xc4, 0x9b, 0x95), // 10 base0A
	tcell.NewRGBColor(0x7a, 0x83, 0x7c), // 11 base0B
	tcell.NewRGBColor(0x6d, 0x75, 0x80), // 12 base0C
	tcell.NewRGBColor(0x69, 0x7a, 0x9a), // 13 base0D
	tcell.NewRGBColor(0x83, 0x79, 0x9c), // 14 base0E
	tcell.NewRGBColor(0x55, 0x4a, 0x62), // 15 base0F
}

// dimColor scales an RGB triple to ~28% brightness, preserving the hue.
func dimColor(r, g, b int) tcell.Color {
	const k = 0.25
	return tcell.NewRGBColor(int32(float32(r)*k), int32(float32(g)*k), int32(float32(b)*k))
}

var baseDim = []tcell.Color{
	dimColor(0x10, 0x11, 0x13), dimColor(0x16, 0x17, 0x19), dimColor(0x1e, 0x1f, 0x22),
	dimColor(0x55, 0x4a, 0x62), dimColor(0x78, 0x74, 0x87), dimColor(0x9f, 0x9e, 0x99),
	dimColor(0xb5, 0xb3, 0xb0), dimColor(0xe1, 0xff, 0xe5), dimColor(0x6f, 0x2e, 0x2a),
	dimColor(0xac, 0x7f, 0x7b), dimColor(0xc4, 0x9b, 0x95), dimColor(0x7a, 0x83, 0x7c),
	dimColor(0x6d, 0x75, 0x80), dimColor(0x69, 0x7a, 0x9a), dimColor(0x83, 0x79, 0x9c),
	dimColor(0x55, 0x4a, 0x62),
}

var charColor = map[rune]int{
	' ': 0, '.': 1, ':': 2, ';': 3, '\'': 4, ',': 5, 'w': 6, 'i': 7,
	'o': 8, 'g': 9, 'O': 10, 'L': 11, 'X': 12, 'H': 13, 'W': 14, 'Y': 15,
	'V': 15, '@': 15, '+': 7, '*': 10, '·': 1, '⠂': 1, '⠈': 0,
}

var cloudWhite = []tcell.Color{
	tcell.NewRGBColor(0x6e, 0x6e, 0x78), tcell.NewRGBColor(0x8a, 0x8a, 0x92),
	tcell.NewRGBColor(0xa6, 0xa6, 0xac), tcell.NewRGBColor(0xc0, 0xc0, 0xc4),
	tcell.NewRGBColor(0xd8, 0xd8, 0xda), tcell.NewRGBColor(0xe8, 0xe8, 0xea),
}

var cloudGrey = map[rune]int{
	'.': 0, ':': 1, '\'': 2, ',': 3, 'w': 4, 'W': 5,
	'·': 5, '*': 5, '#': 5, '░': 5, 'o': 5,
}

func clamp255(v int32) int32 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func scaleColor(c tcell.Color, k float32) tcell.Color {
	r, g, b := c.RGB()
	return tcell.NewRGBColor(clamp255(int32(float32(r)*k)), clamp255(int32(float32(g)*k)), clamp255(int32(float32(b)*k)))
}

func colorToANSI(c tcell.Color, isBg bool) string {
	r, g, b := c.RGB()
	if isBg {
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

func styleForCell(r rune, night, cloud bool) (fg, bg tcell.Color) {
	idx, ok := charColor[r]
	if !ok {
		idx = 0
	}
	isCity := r == '+' || r == '*'
	isCloud := cloud && !isCity

	bg = base16[0]

	if isCity {
		fg = base16[idx]
		return
	}

	if isCloud {
		cidx, ok := cloudGrey[r]
		if !ok {
			cidx = 2
		}
		if night {
			fg = scaleColor(cloudWhite[cidx], cfg.CloudNightOpacity)
			bg = baseDim[0]
			return
		}
		fg = scaleColor(cloudWhite[cidx], cfg.CloudOpacity)
		return
	}

	if night {
		fg = baseDim[idx]
		bg = baseDim[0]
		return
	}

	fg = base16[idx]
	return
}

func renderGlow(canvas *globe.Canvas, rows, cols int) {
	const maxLayer = 3
	dist := make([][]int, rows)
	for i := range dist {
		dist[i] = make([]int, cols)
		for j := range dist[i] {
			dist[i][j] = -1
		}
	}

	queue := make([][2]int, 0, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if canvas.Matrix[i][j] != ' ' {
				dist[i][j] = 0
				queue = append(queue, [2]int{i, j})
			}
		}
	}

	dirs := [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}
	for head := 0; head < len(queue); head++ {
		p := queue[head]
		if dist[p[0]][p[1]] >= maxLayer {
			continue
		}
		for _, d := range dirs {
			ni, nj := p[0]+d[0], p[1]+d[1]
			if ni >= 0 && ni < rows && nj >= 0 && nj < cols && dist[ni][nj] == -1 {
				dist[ni][nj] = dist[p[0]][p[1]] + 1
				queue = append(queue, [2]int{ni, nj})
			}
		}
	}

	layers := []rune{'·', '⠂', '⠈'}
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if canvas.Matrix[i][j] != ' ' {
				continue
			}
			d := dist[i][j]
			if d >= 1 && d <= maxLayer {
				canvas.Matrix[i][j] = layers[d-1]
			}
		}
	}
}

type HomeModel struct {
	form    *huh.Form
	onReady func(JoinResult) tea.Cmd

	g         *globe.Globe
	spinAngle float32
	orbit     float32
	width     int
	height    int
}

// formFieldKeys is the order of inputs in the home form. It drives arrow-key
// navigation between fields (up/down) and prevents the form from being
// submitted by accident when pressing down on the last field.
var formFieldKeys = []string{"Username", "Room ID", "Server URL"}

// currentFieldIndex returns the index of the focused field within
// formFieldKeys, so navigation decisions stay in sync with the form's real
// state (which can also advance via tab/enter/shift+tab).
func (m *HomeModel) currentFieldIndex() int {
	key := m.form.GetFocusedField().GetKey()
	for i, k := range formFieldKeys {
		if k == key {
			return i
		}
	}
	return 0
}

func NewHomeModel(onReady func(JoinResult) tea.Cmd) *HomeModel {
	var roomID, username, serverURL string
	serverURL = DefaultServerURL

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("Username").
				Title("Username").
				Value(&username).
				Validate(func(s string) error {
					if len(s) == 0 {
						return errors.New("username cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Key("Room ID").
				Title("Room ID").
				Value(&roomID).
				Validate(func(s string) error {
					if len(s) == 0 {
						return errors.New("room ID cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Key("Server URL").
				Title("Server URL").
				Value(&serverURL).
				Validate(func(s string) error {
					if len(s) == 0 {
						return errors.New("server URL cannot be empty")
					}
					return nil
				}),
		).WithShowHelp(false),
	).WithTheme(huh.ThemeDracula()).WithWidth(50)

	form.Init()

	g := globe.NewGlobeConfig().
		UseTemplate(globe.Earth).
		WithCamera(globe.NewCameraConfig(cfg.Zoom, cfg.TiltY, cfg.TiltX)).
		DisplayNight(cfg.Night).
		DisplayClouds(cfg.Clouds).
		Build()

	// Override the brightest cloud glyph from config.
	if cfg.CloudCharBright != "" {
		newChar := []rune(cfg.CloudCharBright)[0]
		for y, row := range g.Texture.Clouds {
			for x, c := range row {
				if c == 'W' {
					g.Texture.Clouds[y][x] = newChar
				}
			}
		}
		cloudGrey[newChar] = 5
	}

	return &HomeModel{
		form:      form,
		onReady:   onReady,
		g:         g,
		spinAngle: cfg.TiltZ,
		orbit:     cfg.TiltY,
	}
}

func (m *HomeModel) Init() tea.Cmd {
	return tea.Batch(m.form.Init(), tickGlobe())
}

func (m *HomeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up":
			// Arrow up: move focus to the previous input field.
			if m.form.State == huh.StateNormal {
				return m, m.form.PrevField()
			}
		case "down":
			// Arrow down: move focus to the next input field. On the last field
			// this is a no-op so the form is not submitted by accident (use
			// enter to submit).
			if m.form.State == huh.StateNormal && m.currentFieldIndex() < len(formFieldKeys)-1 {
				return m, m.form.NextField()
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case GlobeTickMsg:
		m.spinAngle += cfg.RotationSpeed / 1000
		m.orbit += cfg.CamOrbitSpeed / 1000
		m.g.Angle = m.spinAngle
		m.g.CloudAngle += cfg.CloudSpeed / 1000
		m.g.Camera.Update(cfg.Zoom, m.orbit, cfg.TiltX)
		cmds = append(cmds, tickGlobe())
	}

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f

		if m.form.State == huh.StateCompleted {
			res := JoinResult{
				Username:  m.form.GetString("Username"),
				RoomID:    m.form.GetString("Room ID"),
				ServerURL: m.form.GetString("Server URL"),
			}
			return m, m.onReady(res)
		}
	}
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *HomeModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// 1. Render Foreground (Form)
	theme := GetCurrentTheme()
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.TitleFg).
		Background(theme.TitleBg).
		Padding(0, 1).
		Render(" TermCall ")

	formView := m.form.View()

	controls := lipgloss.NewStyle().
		Foreground(theme.ControlBarFg).
		Render("↑/↓ switch  •  shift+tab back  •  enter submit  •  ctrl+c quit")

	fg := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.BorderColor).
		Background(theme.ControlBarBg). // base01 dark panel
		Render(lipgloss.JoinVertical(lipgloss.Center, title, "\n", formView, "\n", controls))

	fgLines := strings.Split(fg, "\n")
	fgHeight := lipgloss.Height(fg)
	fgWidth := lipgloss.Width(fg)

	startY := (m.height - fgHeight) / 2
	if startY < 0 {
		startY = 0
	}

	startX := (m.width - fgWidth) / 2
	if startX < 0 {
		startX = 0
	}

	// 2. Render globe canvas. The canvas is kept visually square (so the globe
	// stays circular and is never stretched). It is sized to the terminal's
	// smaller dimension multiplied by cfg.CanvasScale (so >1.0 lets the globe
	// overflow and be clipped for close-ups), centered on screen, then shifted
	// by cfg.OffsetX/Y. The empty space around it is painted with the globe's
	// own base background color so the whole terminal is covered seamlessly.
	const charPixX, charPixY = 4, 8 // matches globe.Canvas default CharPix
	physW := m.width * charPixX
	physH := m.height * charPixY
	baseSide := physW
	if physH < baseSide {
		baseSide = physH
	}
	side := int(float32(baseSide) * cfg.CanvasScale)
	if side < 16 {
		side = 16
	}
	canvas := globe.NewCanvas(side, side, nil)

	canvas.Clear()
	m.g.RenderOn(canvas)

	canvasSizeX, canvasSizeY := canvas.GetSize()
	rows := canvasSizeY / canvas.CharPix[1]
	cols := canvasSizeX / canvas.CharPix[0]

	renderGlow(canvas, rows, cols)

	// Center the square canvas inside the terminal, then apply the configured
	// offset. Cells outside the canvas are filled with the base background.
	offsetX := (m.width-cols)/2 + cfg.OffsetX
	offsetY := (m.height-rows)/2 + cfg.OffsetY

	// 3. Composite background and foreground.
	var sb strings.Builder

	// Base background color used for the empty area around the globe: the same
	// base00 color the globe paints for its space cells, so the box blends in.
	fillBg := colorToANSI(base16[0], true)
	fillFg := colorToANSI(base16[0], false)

	for i := 0; i < m.height; i++ {
		// Check if we are on a row that contains the foreground
		inFgRow := i >= startY && i < startY+fgHeight

		for j := 0; j < m.width; j++ {
			// If we hit the foreground box, inject the foreground line and skip ahead
			if inFgRow && j == startX {
				sb.WriteString(fgLines[i-startY])
				sb.WriteString("\x1b[0m") // ensure reset
				j += fgWidth - 1          // skip the width of the foreground
				continue
			}

			ci := i - offsetY
			cj := j - offsetX
			if ci >= 0 && ci < rows && cj >= 0 && cj < cols {
				ch := canvas.Matrix[ci][cj]
				night := canvas.Night[ci][cj]
				cloud := canvas.Cloud[ci][cj]

				fgColor, bgColor := styleForCell(ch, night, cloud)
				sb.WriteString(colorToANSI(bgColor, true))
				sb.WriteString(colorToANSI(fgColor, false))
				sb.WriteRune(ch)
			} else {
				// Outside the globe canvas: paint the base background so the
				// whole terminal is filled.
				sb.WriteString(fillBg)
				sb.WriteString(fillFg)
				sb.WriteRune(' ')
			}
		}
		sb.WriteString("\x1b[0m\n")
	}

	return sb.String()
}
