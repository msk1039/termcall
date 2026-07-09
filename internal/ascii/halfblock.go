package ascii

import (
	"fmt"
	"image"
	"strings"
)

// renderHalfBlockCells samples img into a cell grid where each terminal row
// covers two image rows: the top pixel becomes the background colour and the
// bottom pixel becomes the foreground colour of a half-block (▄) cell.
func (r *ColorRenderer) renderHalfBlockCells(img image.Image, targetWidth, targetHeight int) [][]Cell {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	if targetWidth <= 0 {
		targetWidth = 80
	}
	if targetHeight <= 0 {
		targetHeight = 24
	}

	virtualHeight := targetHeight * 2
	xScale := float64(width) / float64(targetWidth)
	yScale := float64(height) / float64(virtualHeight)

	cells := make([][]Cell, targetHeight)
	for y := 0; y < targetHeight; y++ {
		row := make([]Cell, targetWidth)
		vyTop := y * 2
		vyBot := y*2 + 1
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*xScale)
			srcYTop := bounds.Min.Y + int(float64(vyTop)*yScale)
			srcYBot := bounds.Min.Y + int(float64(vyBot)*yScale)
			tr, tg, tb, _ := img.At(srcX, srcYTop).RGBA()
			br, bg, bb, _ := img.At(srcX, srcYBot).RGBA()
			row[x] = Cell{
				Char: 0,
				Bg:   rgbToANSI256(tr, tg, tb),
				Fg:   rgbToANSI256(br, bg, bb),
			}
		}
		cells[y] = row
	}
	return cells
}

// serialiseHalfBlock writes a cell grid as half-block (▄) characters with
// 256-colour foreground and background, skipping redundant escapes.
func serialiseHalfBlock(cells [][]Cell) string {
	if len(cells) == 0 {
		return ""
	}
	h := len(cells)
	w := len(cells[0])
	var sb strings.Builder
	sb.Grow(h * w * 25)
	lastBg := -1
	lastFg := -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y][x]
			
			if c.Char == ' ' {
				if lastBg != -1 || lastFg != -1 {
					sb.WriteString("\x1b[0m")
					lastBg = -1
					lastFg = -1
				}
				sb.WriteString(" ")
				continue
			}

			if c.Bg != lastBg {
				fmt.Fprintf(&sb, "\x1b[48;5;%dm", c.Bg)
				lastBg = c.Bg
			}
			if c.Fg != lastFg {
				fmt.Fprintf(&sb, "\x1b[38;5;%dm", c.Fg)
				lastFg = c.Fg
			}
			sb.WriteString("▄")
		}
		if y < h-1 {
			sb.WriteString("\x1b[0m\n")
			lastBg, lastFg = -1, -1
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}
