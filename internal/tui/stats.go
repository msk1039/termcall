package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/msk1039/termcall/internal/rtc"
)

func renderOutgoingStats(stats rtc.PeerStats, theme Theme) string {
	boxStyle := lipgloss.NewStyle().
		Background(theme.StatsBg).
		Foreground(theme.StatusFg).
		Padding(0, 1)

	upArrow := lipgloss.NewStyle().Foreground(theme.StatsArrowUp).Render("▲")
	total := stats.OutgoingKBps + stats.AudioOutKBps

	content := fmt.Sprintf("%s UP: %.1f KB/s (V:%.1f A:%.1f)", upArrow, total, stats.OutgoingKBps, stats.AudioOutKBps)
	return boxStyle.Render(content)
}

func renderIncomingStats(stats rtc.PeerStats, theme Theme) string {
	boxStyle := lipgloss.NewStyle().
		Background(theme.StatsBg).
		Foreground(theme.StatusFg).
		Padding(0, 1)

	downArrow := lipgloss.NewStyle().Foreground(theme.StatsArrowDown).Render("▼")
	total := stats.IncomingKBps + stats.AudioInKBps

	content := fmt.Sprintf("%s DOWN: %.1f KB/s (V:%.1f A:%.1f)", downArrow, total, stats.IncomingKBps, stats.AudioInKBps)
	return boxStyle.Render(content)
}
