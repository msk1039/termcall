package ascii

import (
	"image"
	"math"
)

// RenderMode defines how video frames are converted to strings.
type RenderMode int

const (
	ModeASCII     RenderMode = iota // monochrome ASCII characters
	ModeColor256                    // ASCII chars with 256-color foreground
	ModeHalfBlock                   // half-block chars with 256-color fg+bg
)

// Renderer converts a raw image frame to a terminal string.
type Renderer interface {
	Convert(img image.Image, targetWidth, targetHeight int) string
}

// DefaultGradient is the character ramp from dark to light.
const DefaultGradient = " .:-=+*#%@"

// rgbToANSI256 maps a 16-bit RGB triplet to a 256-color ANSI index.
func rgbToANSI256(r, g, b uint32) int {
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

// ColorRenderer converts an image into ANSI-coloured terminal output. It
// separates colour computation (a Cell grid) from serialisation, runs a noise-
// suppression stabiliser over the grid, and delta-encodes the result for
// bandwidth-efficient transport.
type ColorRenderer struct {
	mode       RenderMode
	gradient   string
	stabiliser *Stabiliser
	delta      *DeltaEncoder
}

// NewColorRenderer creates a renderer for the given mode with noise suppression
// (threshold 1 ANSI step) and delta compression (keyframe every ~2s at 15fps).
func NewColorRenderer(mode RenderMode) *ColorRenderer {
	return &ColorRenderer{
		mode:       mode,
		gradient:   DefaultGradient,
		stabiliser: NewStabiliser(1),
		delta:      NewDeltaEncoder(mode, 30),
	}
}

func (r *ColorRenderer) SetMode(mode RenderMode) {
	r.mode = mode
	if r.delta != nil {
		r.delta.mode = mode
		r.delta.Reset()
	}
	if r.stabiliser != nil {
		r.stabiliser.Reset()
	}
}

func (r *ColorRenderer) GetMode() RenderMode {
	return r.mode
}

// renderCells produces the cell grid for the current mode.
func (r *ColorRenderer) renderCells(img image.Image, w, h int) [][]Cell {
	switch r.mode {
	case ModeASCII:
		return r.renderASCIICells(img, w, h)
	case ModeColor256:
		return r.renderColor256Cells(img, w, h)
	case ModeHalfBlock:
		return r.renderHalfBlockCells(img, w, h)
	}
	return nil
}

// serialise converts a cell grid to an ANSI string for the current mode.
func (r *ColorRenderer) serialise(cells [][]Cell) string {
	switch r.mode {
	case ModeASCII:
		return serialiseASCII(cells)
	case ModeColor256:
		return serialiseColor256(cells)
	case ModeHalfBlock:
		return serialiseHalfBlock(cells)
	}
	return ""
}

// SerialiseMode converts a cell grid to an ANSI string for a given mode.
func SerialiseMode(cells [][]Cell, mode RenderMode) string {
	switch mode {
	case ModeASCII:
		return serialiseASCII(cells)
	case ModeColor256:
		return serialiseColor256(cells)
	case ModeHalfBlock:
		return serialiseHalfBlock(cells)
	}
	return ""
}

// Convert renders an image to a full ANSI display string (noise-suppressed).
// This is the display path; it does not delta-encode.
func (r *ColorRenderer) Convert(img image.Image, targetWidth, targetHeight int) string {
	cells := r.renderCells(img, targetWidth, targetHeight)
	if r.stabiliser != nil {
		cells = r.stabiliser.Stabilise(cells)
	}
	return r.serialise(cells)
}

// ConvertForSend renders an image and returns both the full ANSI display
// string (for local display) and a delta-encoded string (for the network).
func (r *ColorRenderer) ConvertForSend(img image.Image, w, h int) (displayStr, sendStr string) {
	cells := r.renderCells(img, w, h)
	if r.stabiliser != nil {
		cells = r.stabiliser.Stabilise(cells)
	}
	displayStr = r.serialise(cells)
	if r.delta != nil {
		sendStr = r.delta.Encode(cells)
	} else {
		sendStr = displayStr
	}
	return
}

// ConvertCells renders an image to a stabilised cell grid and delta-encoded
// send string. The cell grid can be upscaled at display time.
func (r *ColorRenderer) ConvertCells(img image.Image, w, h int) (cells [][]Cell, sendStr string) {
	cells = r.renderCells(img, w, h)
	if r.stabiliser != nil {
		cells = r.stabiliser.Stabilise(cells)
	}
	if r.delta != nil {
		sendStr = r.delta.Encode(cells)
	} else {
		sendStr = r.serialise(cells)
	}
	return
}
