package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
)

func renderControls(termW int, camOn, micOn, showStats bool, renderMode string, theme Theme) string {
	// Left segments
	vidBg := theme.InactiveBtnBg
	vidFg := theme.InactiveBtnFg
	if camOn {
		vidBg = theme.ActiveBtnBg
		vidFg = theme.ActiveBtnFg
	}

	micBg := theme.InactiveBtnBg
	micFg := theme.InactiveBtnFg
	if micOn {
		micBg = theme.ActiveBtnBg
		micFg = theme.ActiveBtnFg
	}

	vidText := " [V] Video OFF "
	if camOn {
		vidText = " [V] Video ON "
	}
	micText := " [M] Mic OFF "
	if micOn {
		micText = " [M] Mic ON "
	}

	// Left: Video -> Mic
	p1 := lipgloss.NewStyle().Background(vidBg).Foreground(vidFg).Bold(true).Render(vidText)
	p1Sep := lipgloss.NewStyle().Background(micBg).Foreground(vidBg).Render("")
	p2 := lipgloss.NewStyle().Background(micBg).Foreground(micFg).Bold(true).Render(micText)
	p2Sep := lipgloss.NewStyle().Background(theme.ControlBarBg).Foreground(micBg).Render("")

	leftSection := lipgloss.JoinHorizontal(lipgloss.Top, p1, p1Sep, p2, p2Sep)

	// Right segments: [N] Mode -> [S] Stats -> [T] Theme -> [Q] Quit
	// We'll build from right to left to get the separators right.
	qBg := theme.TitleBg
	qFg := theme.TitleFg
	tBg := theme.StatsArrowDown
	tFg := theme.TitleFg
	sBg := theme.StatsBg
	sFg := theme.StatusFg
	if showStats {
		sBg = theme.ActiveBtnBg
		sFg = theme.ActiveBtnFg
	}
	nBg := theme.BorderColor
	nFg := theme.TitleFg

	qText := " [Q] Quit "
	tText := " [T] Theme "
	sText := " [S] Stats "
	nText := fmt.Sprintf(" [N] %s ", renderMode)

	r1Sep := lipgloss.NewStyle().Background(theme.ControlBarBg).Foreground(nBg).Render("")
	r1 := lipgloss.NewStyle().Background(nBg).Foreground(nFg).Bold(true).Render(nText)
	
	r2Sep := lipgloss.NewStyle().Background(nBg).Foreground(sBg).Render("")
	r2 := lipgloss.NewStyle().Background(sBg).Foreground(sFg).Bold(true).Render(sText)

	r3Sep := lipgloss.NewStyle().Background(sBg).Foreground(tBg).Render("")
	r3 := lipgloss.NewStyle().Background(tBg).Foreground(tFg).Bold(true).Render(tText)

	r4Sep := lipgloss.NewStyle().Background(tBg).Foreground(qBg).Render("")
	r4 := lipgloss.NewStyle().Background(qBg).Foreground(qFg).Bold(true).Render(qText)

	rightSection := lipgloss.JoinHorizontal(lipgloss.Top, r1Sep, r1, r2Sep, r2, r3Sep, r3, r4Sep, r4)

	// Center spacing
	leftW := lipgloss.Width(leftSection)
	rightW := lipgloss.Width(rightSection)
	spacerW := termW - leftW - rightW
	if spacerW < 0 {
		spacerW = 0
	}
	spacer := lipgloss.NewStyle().Background(theme.ControlBarBg).Width(spacerW).Render("")

	content := lipgloss.JoinHorizontal(lipgloss.Top, leftSection, spacer, rightSection)

	return lipgloss.NewStyle().
		Width(termW).
		Background(theme.ControlBarBg).
		Render(content)
}
