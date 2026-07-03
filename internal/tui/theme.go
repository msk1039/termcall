package tui

import "github.com/charmbracelet/lipgloss"

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

var midnightTheme = Theme{
	Name:           "midnight",
	TitleFg:        lipgloss.Color("#FAFAFA"),
	TitleBg:        lipgloss.Color("#7D56F4"),
	ControlBarBg:   lipgloss.Color("#1E1E2E"),
	ControlBarFg:   lipgloss.Color("#CDD6F4"),
	ActiveBtnFg:    lipgloss.Color("#1E1E2E"),
	ActiveBtnBg:    lipgloss.Color("#A6E3A1"),
	InactiveBtnFg:  lipgloss.Color("#CDD6F4"),
	InactiveBtnBg:  lipgloss.Color("#F38BA8"),
	BorderColor:    lipgloss.Color("#89B4FA"),
	PeerLabelFg:    lipgloss.Color("#89DCEB"),
	StatusFg:       lipgloss.Color("#F9E2AF"),
	StatsBg:        lipgloss.Color("#181825"),
	VolumeOn:       lipgloss.Color("#A6E3A1"),
	VolumeOff:      lipgloss.Color("#585B70"),
	StatsArrowUp:   lipgloss.Color("#A6E3A1"),
	StatsArrowDown: lipgloss.Color("#89B4FA"),
}

var daylightTheme = Theme{
	Name:           "daylight",
	TitleFg:        lipgloss.Color("#111111"),
	TitleBg:        lipgloss.Color("#00BFFF"),
	ControlBarBg:   lipgloss.Color("#E0E0E0"),
	ControlBarFg:   lipgloss.Color("#333333"),
	ActiveBtnFg:    lipgloss.Color("#FFFFFF"),
	ActiveBtnBg:    lipgloss.Color("#2E8B57"),
	InactiveBtnFg:  lipgloss.Color("#FFFFFF"),
	InactiveBtnBg:  lipgloss.Color("#DC143C"),
	BorderColor:    lipgloss.Color("#4682B4"),
	PeerLabelFg:    lipgloss.Color("#000080"),
	StatusFg:       lipgloss.Color("#DAA520"),
	StatsBg:        lipgloss.Color("#F5F5F5"),
	VolumeOn:       lipgloss.Color("#2E8B57"),
	VolumeOff:      lipgloss.Color("#A9A9A9"),
	StatsArrowUp:   lipgloss.Color("#2E8B57"),
	StatsArrowDown: lipgloss.Color("#4169E1"),
}

var ayuTheme = Theme{
	Name:           "ayu",
	TitleFg:        lipgloss.Color("#E6E1CF"),
	TitleBg:        lipgloss.Color("#FFB454"),
	ControlBarBg:   lipgloss.Color("#14191F"),
	ControlBarFg:   lipgloss.Color("#E6E1CF"),
	ActiveBtnFg:    lipgloss.Color("#0F1419"),
	ActiveBtnBg:    lipgloss.Color("#91B362"),
	InactiveBtnFg:  lipgloss.Color("#E6E1CF"),
	InactiveBtnBg:  lipgloss.Color("#F07178"),
	BorderColor:    lipgloss.Color("#3E4B59"),
	PeerLabelFg:    lipgloss.Color("#39BAE6"),
	StatusFg:       lipgloss.Color("#E6B673"),
	StatsBg:        lipgloss.Color("#0F1419"),
	VolumeOn:       lipgloss.Color("#91B362"),
	VolumeOff:      lipgloss.Color("#5C6773"),
	StatsArrowUp:   lipgloss.Color("#91B362"),
	StatsArrowDown: lipgloss.Color("#39BAE6"),
}

var catppuccinTheme = Theme{
	Name:           "catppuccin",
	TitleFg:        lipgloss.Color("#11111B"),
	TitleBg:        lipgloss.Color("#CBA6F7"),
	ControlBarBg:   lipgloss.Color("#181825"),
	ControlBarFg:   lipgloss.Color("#CDD6F4"),
	ActiveBtnFg:    lipgloss.Color("#11111B"),
	ActiveBtnBg:    lipgloss.Color("#A6E3A1"),
	InactiveBtnFg:  lipgloss.Color("#CDD6F4"),
	InactiveBtnBg:  lipgloss.Color("#F38BA8"),
	BorderColor:    lipgloss.Color("#89B4FA"),
	PeerLabelFg:    lipgloss.Color("#89DCEB"),
	StatusFg:       lipgloss.Color("#F9E2AF"),
	StatsBg:        lipgloss.Color("#1E1E2E"),
	VolumeOn:       lipgloss.Color("#A6E3A1"),
	VolumeOff:      lipgloss.Color("#585B70"),
	StatsArrowUp:   lipgloss.Color("#A6E3A1"),
	StatsArrowDown: lipgloss.Color("#89B4FA"),
}

var themes = []Theme{midnightTheme, daylightTheme, ayuTheme, catppuccinTheme}
var currentThemeIdx = 2 // default to ayu

// GetCurrentTheme returns the currently active theme
func GetCurrentTheme() Theme {
	return themes[currentThemeIdx]
}

// NextTheme cycles to the next theme and returns it
func NextTheme() Theme {
	currentThemeIdx = (currentThemeIdx + 1) % len(themes)
	return themes[currentThemeIdx]
}
