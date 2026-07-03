package ascii

import (
	"fmt"
	"image"
	"math"
	"strings"
)

// RenderMode defines how video frames are converted to strings
type RenderMode int

const (
	ModeASCII RenderMode = iota // monochrome ASCII characters
	ModeColor256                 // ASCII characters with 256-color ANSI foreground
	ModeHalfBlock                // Unicode half-blocks with 256-color ANSI foreground and background
)

// ColorRenderer converts an image into an ANSI colored string
type ColorRenderer struct {
	mode     RenderMode
	gradient string
}

func NewColorRenderer(mode RenderMode) *ColorRenderer {
	return &ColorRenderer{
		mode:     mode,
		gradient: " .:-=+*#%@",
	}
}

func (r *ColorRenderer) SetMode(mode RenderMode) {
	r.mode = mode
}

func (r *ColorRenderer) GetMode() RenderMode {
	return r.mode
}

func rgbToANSI256(r, g, b uint32) int {
	// r, g, b are 16-bit (0-65535)
	r8 := int(r >> 8)
	g8 := int(g >> 8)
	b8 := int(b >> 8)

	if r8 == g8 && g8 == b8 {
		if r8 < 8 {
			return 16
		}
		if r8 > 248 {
			return 231
		}
		return int(math.Round(float64(r8-8)/247.0*24.0)) + 232
	}

	return 16 + 36*int(math.Round(float64(r8)/255.0*5.0)) + 6*int(math.Round(float64(g8)/255.0*5.0)) + int(math.Round(float64(b8)/255.0*5.0))
}

func (r *ColorRenderer) convertASCII(img image.Image, targetWidth, targetHeight int) string {
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

	var sb strings.Builder
	sb.Grow(targetHeight * (targetWidth + 1))

	gradient := r.gradient

	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*xScale)
			srcY := bounds.Min.Y + int(float64(y)*yScale)

			rColor, g, b, _ := img.At(srcX, srcY).RGBA()
			luminance := (0.299*float64(rColor) + 0.587*float64(g) + 0.114*float64(b)) / 256.0

			idx := int((luminance / 255.0) * float64(len(gradient)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(gradient) {
				idx = len(gradient)-1
			}

			sb.WriteByte(gradient[idx])
		}
		if y < targetHeight-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

func (r *ColorRenderer) convertColor256(img image.Image, targetWidth, targetHeight int) string {
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

	var sb strings.Builder
	// Rough preallocation considering escape sequences
	sb.Grow(targetHeight * targetWidth * 12)

	gradient := r.gradient
	lastColor := -1

	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*xScale)
			srcY := bounds.Min.Y + int(float64(y)*yScale)

			rColor, g, b, _ := img.At(srcX, srcY).RGBA()
			luminance := (0.299*float64(rColor) + 0.587*float64(g) + 0.114*float64(b)) / 256.0

			idx := int((luminance / 255.0) * float64(len(gradient)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(gradient) {
				idx = len(gradient)-1
			}

			color := rgbToANSI256(rColor, g, b)
			if color != lastColor {
				sb.WriteString(fmt.Sprintf("\x1b[38;5;%dm", color))
				lastColor = color
			}
			sb.WriteByte(gradient[idx])
		}
		if y < targetHeight-1 {
			sb.WriteString("\x1b[0m\n")
			lastColor = -1
		}
	}
	sb.WriteString("\x1b[0m")

	return sb.String()
}

func (r *ColorRenderer) convertHalfBlock(img image.Image, targetWidth, targetHeight int) string {
	bounds := img.Bounds()
	width := bounds.Max.X - bounds.Min.X
	height := bounds.Max.Y - bounds.Min.Y

	if targetWidth <= 0 {
		targetWidth = 80
	}
	if targetHeight <= 0 {
		targetHeight = 24
	}

	// We process two virtual rows for each terminal row.
	virtualHeight := targetHeight * 2

	xScale := float64(width) / float64(targetWidth)
	yScale := float64(height) / float64(virtualHeight)

	var sb strings.Builder
	// ANSI sequence size buffer
	sb.Grow(targetHeight * targetWidth * 25)

	lastBg := -1
	lastFg := -1

	for y := 0; y < targetHeight; y++ {
		vyTop := y * 2
		vyBot := y*2 + 1

		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*xScale)
			srcYTop := bounds.Min.Y + int(float64(vyTop)*yScale)
			srcYBot := bounds.Min.Y + int(float64(vyBot)*yScale)

			tr, tg, tb, _ := img.At(srcX, srcYTop).RGBA()
			br, bg, bb, _ := img.At(srcX, srcYBot).RGBA()

			bgColor := rgbToANSI256(tr, tg, tb)
			fgColor := rgbToANSI256(br, bg, bb)

			if bgColor != lastBg {
				sb.WriteString(fmt.Sprintf("\x1b[48;5;%dm", bgColor))
				lastBg = bgColor
			}
			if fgColor != lastFg {
				sb.WriteString(fmt.Sprintf("\x1b[38;5;%dm", fgColor))
				lastFg = fgColor
			}
			sb.WriteString("▄")
		}
		if y < targetHeight-1 {
			sb.WriteString("\x1b[0m\n")
			lastBg = -1
			lastFg = -1
		}
	}
	sb.WriteString("\x1b[0m")

	return sb.String()
}

func (r *ColorRenderer) Convert(img image.Image, targetWidth, targetHeight int) string {
	switch r.mode {
	case ModeASCII:
		return r.convertASCII(img, targetWidth, targetHeight)
	case ModeColor256:
		return r.convertColor256(img, targetWidth, targetHeight)
	case ModeHalfBlock:
		return r.convertHalfBlock(img, targetWidth, targetHeight)
	}
	return ""
}
