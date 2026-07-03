package tui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderVoiceActivity renders a small bar like "▁▂▄▆█" based on RMS volume.
func renderVoiceActivity(rms float64, theme Theme) string {
	// Let's use 5 bars
	// RMS usually is quite small, e.g. 0.01 for silence, 0.1 for talking.
	// We might need to scale it. Let's assume talking is around 0.05 to 0.2.
	// For now, let's use a non-linear scale.
	scaled := rms * 10.0
	if scaled > 1.0 {
		scaled = 1.0
	}

	bars := []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	levels := 5 // 5 bars
	
	// If it's completely silent, show flat lines
	if rms < 0.005 {
		silence := lipgloss.NewStyle().Foreground(theme.VolumeOff).Render(strings.Repeat("▁", levels))
		return " " + silence + " "
	}

	var sb strings.Builder
	for i := 0; i < levels; i++ {
		// A simple visualizer effect: center is highest, edges are lower
		// Distance from center
		dist := math.Abs(float64(i - levels/2))
		
		// Value at this bar (0.0 to 1.0)
		val := scaled - (dist * 0.2)
		if val < 0.05 {
			val = 0.05 // minimum visible bar
		}
		if val > 1.0 {
			val = 1.0
		}

		idx := int(val * float64(len(bars)-1))
		if idx < 0 {
			idx = 0
		} else if idx >= len(bars) {
			idx = len(bars) - 1
		}
		sb.WriteString(bars[idx])
	}

	active := lipgloss.NewStyle().Foreground(theme.VolumeOn).Render(sb.String())
	return " " + active + " "
}
