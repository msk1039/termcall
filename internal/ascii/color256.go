package ascii

import (
	"fmt"
	"image"
	"strings"
)

// renderColor256Cells samples img into a cell grid with a gradient char and a
// 256-colour foreground per cell.
func (r *ColorRenderer) renderColor256Cells(img image.Image, targetWidth, targetHeight int) [][]Cell {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	if targetWidth <= 0 {
		targetWidth = 80
	}
	if targetHeight <= 0 {
		targetHeight = 24
	}

	xScale := float64(width) / float64(targetWidth)
	yScale := float64(height) / float64(targetHeight)
	gradient := r.gradient

	cells := make([][]Cell, targetHeight)
	for y := 0; y < targetHeight; y++ {
		row := make([]Cell, targetWidth)
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*xScale)
			srcY := bounds.Min.Y + int(float64(y)*yScale)
			rColor, g, b, _ := img.At(srcX, srcY).RGBA()
			lum := (0.299*float64(rColor) + 0.587*float64(g) + 0.114*float64(b)) / 256.0
			idx := int((lum / 255.0) * float64(len(gradient)-1))
			if idx < 0 {
				idx = 0
			} else if idx >= len(gradient) {
				idx = len(gradient) - 1
			}
			row[x] = Cell{Char: gradient[idx], Fg: rgbToANSI256(rColor, g, b), Bg: -1}
		}
		cells[y] = row
	}
	return cells
}

// serialiseColor256 writes a cell grid as ANSI-256 foreground text, skipping
// redundant colour escapes (run-length style).
func serialiseColor256(cells [][]Cell) string {
	if len(cells) == 0 {
		return ""
	}
	h := len(cells)
	w := len(cells[0])
	var sb strings.Builder
	sb.Grow(h * w * 12)
	lastFg := -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y][x]
			if c.Fg != lastFg {
				fmt.Fprintf(&sb, "\x1b[38;5;%dm", c.Fg)
				lastFg = c.Fg
			}
			sb.WriteByte(c.Char)
		}
		if y < h-1 {
			sb.WriteString("\x1b[0m\n")
			lastFg = -1
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}
