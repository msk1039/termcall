package tui

import "github.com/charmbracelet/lipgloss"

// Theme is the single color palette used across the whole CLI. It is derived
// from the globe's base16 terminal palette so the UI matches the background
// render. Every background is dark (base00–base03) and every text color is
// light (base06–base07), so contrast is consistent everywhere: light text on
// dark backgrounds.
type Theme struct {
	Name           string
	TitleFg        lipgloss.Color
	TitleBg        lipgloss.Color
	ControlBarBg   lipgloss.Color
	ControlBarFg   lipgloss.Color
	ActiveBtnFg    lipgloss.Color
	ActiveBtnBg    lipgloss.Color
	InactiveBtnFg  lipgloss.Color
	InactiveBtnBg  lipgloss.Color
	BorderColor    lipgloss.Color
	PeerLabelFg    lipgloss.Color
	StatusFg       lipgloss.Color
	StatsBg        lipgloss.Color
	VolumeOn       lipgloss.Color
	VolumeOff      lipgloss.Color
	StatsArrowUp   lipgloss.Color
	StatsArrowDown lipgloss.Color
}

// theme is the one and only theme. base16 palette:
//
//	base00 #101113  base01 #161719  base02 #1e1f22  base03 #554a62
//	base04 #787487  base05 #9f9e99  base06 #b5b3b0  base07 #e1ffe5
//	base08 #6f2e2a  base09 #ac7f7b  base0A #c49b95  base0B #7a837c
//	base0C #6d7580  base0D #697a9a  base0E #83799c  base0F #554a62
var theme = Theme{
	Name: "base16",

	// Title bar: light text on a muted mauve accent.
	TitleFg: lipgloss.Color("#e1ffe5"), // base07
	TitleBg: lipgloss.Color("#83799c"), // base0E

	// Main control panel: light text on the dark base panel.
	ControlBarBg: lipgloss.Color("#161719"), // base01
	ControlBarFg: lipgloss.Color("#b5b3b0"), // base06

	// Buttons: light text on the active (green, on) / inactive (red, off) accents.
	ActiveBtnFg:   lipgloss.Color("#e1ffe5"), // base07
	ActiveBtnBg:   lipgloss.Color("#7a837c"), // base0B sage (on)
	InactiveBtnFg: lipgloss.Color("#e1ffe5"), // base07
	InactiveBtnBg: lipgloss.Color("#6f2e2a"), // base08 dark red (off)

	BorderColor: lipgloss.Color("#554a62"), // base03
	PeerLabelFg: lipgloss.Color("#697a9a"), // base0D muted blue

	// Stats panel: warm peach text on a dark panel.
	StatusFg: lipgloss.Color("#c49b95"), // base0A
	StatsBg:  lipgloss.Color("#1e1f22"), // base02

	VolumeOn:  lipgloss.Color("#7a837c"), // base0B (on)
	VolumeOff: lipgloss.Color("#554a62"), // base03 (off)

	StatsArrowUp:   lipgloss.Color("#7a837c"), // base0B green
	StatsArrowDown: lipgloss.Color("#ac7f7b"), // base09 red
}

// GetCurrentTheme returns the single active theme.
func GetCurrentTheme() Theme {
	return theme
}
