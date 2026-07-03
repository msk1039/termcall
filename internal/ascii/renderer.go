package ascii

import (
	"image"
	"strings"
)

// Renderer converts a raw image frame to an ASCII string representation.
type Renderer interface {
	// Convert transforms an image into an ASCII string of the given
	// terminal dimensions (width in columns, height in rows).
	Convert(img image.Image, targetWidth, targetHeight int) string
}

// Config holds tunable parameters for a renderer.
type Config struct {
	Gradient  string // character set from darkest to lightest
	Invert    bool   // flip luminance mapping
	ColorMode string // "none", "ansi256", "truecolor" (future)
}

// DefaultRenderer is the standard ASCII renderer using luminance.
type DefaultRenderer struct {
	config Config
}

// NewDefaultRenderer creates a new DefaultRenderer.
func NewDefaultRenderer(config Config) *DefaultRenderer {
	if config.Gradient == "" {
		config.Gradient = " .:-=+*#%@"
	}
	return &DefaultRenderer{config: config}
}

// Convert implements the Renderer interface.
func (r *DefaultRenderer) Convert(img image.Image, targetWidth, targetHeight int) string {
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

	gradient := r.config.Gradient
	if r.config.Invert {
		// Reverse gradient
		runes := []rune(gradient)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		gradient = string(runes)
	}

	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + int(float64(x)*xScale)
			srcY := bounds.Min.Y + int(float64(y)*yScale)

			rColor, g, b, _ := img.At(srcX, srcY).RGBA()

			// Luminance formula
			luminance := (0.299*float64(rColor) + 0.587*float64(g) + 0.114*float64(b)) / 256.0

			idx := int((luminance / 255.0) * float64(len(gradient)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(gradient) {
				idx = len(gradient) - 1
			}

			sb.WriteByte(gradient[idx])
		}
		if y < targetHeight-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
