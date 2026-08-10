package ascii

import (
	"image"
	"image/color"
	"testing"
)

func TestColorRendererModesRoundTrip(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 4, 4))
	colors := []color.RGBA{
		{R: 0, G: 0, B: 0, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 128, B: 255, A: 255},
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			frame.SetRGBA(x, y, colors[(x+y)%len(colors)])
		}
	}

	renderer := NewColorRenderer(ModeASCII)
	for _, mode := range []RenderMode{ModeASCII, ModeColor256, ModeHalfBlock} {
		renderer.SetMode(mode)
		if renderer.GetMode() != mode {
			t.Fatalf("mode = %d, want %d", renderer.GetMode(), mode)
		}
		cells, encoded := renderer.ConvertCells(frame, 4, 2)
		if len(cells) != 2 || len(cells[0]) != 4 || encoded == "" {
			t.Fatalf("mode %d produced invalid cells or frame", mode)
		}
		decoded, decodedMode, err := NewDeltaDecoder().DecodeCells(encoded)
		if err != nil {
			t.Fatalf("mode %d decode: %v", mode, err)
		}
		if decodedMode != mode || !cellsEqual(decoded, cells) {
			t.Fatalf("mode %d round trip mismatch", mode)
		}
		if display := renderer.Convert(frame, 4, 2); display == "" {
			t.Fatalf("mode %d display was empty", mode)
		}
		display, nextEncoded := renderer.ConvertForSend(frame, 4, 2)
		if display == "" || nextEncoded == "" {
			t.Fatalf("mode %d ConvertForSend returned empty data", mode)
		}
		renderer.Reset()
	}
}

func TestRendererDefaultsAndFallbackSerialization(t *testing.T) {
	frame := image.NewUniform(color.RGBA{R: 20, G: 40, B: 60, A: 255})
	renderer := NewColorRenderer(ModeASCII)
	cells := renderer.renderCells(frame, 0, 0)
	if len(cells) != 24 || len(cells[0]) != 80 {
		t.Fatalf("default grid = %dx%d", len(cells[0]), len(cells))
	}

	renderer.delta = nil
	convertedCells, encoded := renderer.ConvertCells(frame, 2, 1)
	if encoded != serialiseASCII(convertedCells) {
		t.Fatal("ConvertCells fallback did not serialize the full frame")
	}
	_, encoded = renderer.ConvertForSend(frame, 2, 1)
	if encoded == "" {
		t.Fatal("ConvertForSend fallback was empty")
	}

	renderer.SetMode(RenderMode(99))
	if renderer.renderCells(frame, 1, 1) != nil || renderer.serialise(convertedCells) != "" || SerialiseMode(convertedCells, RenderMode(99)) != "" {
		t.Fatal("invalid render mode unexpectedly produced output")
	}
}

func TestRGBToANSI256RangeAndGrayBranches(t *testing.T) {
	tests := [][3]uint32{
		{0, 0, 0},
		{0xffff, 0xffff, 0xffff},
		{0x8080, 0x8080, 0x8080},
		{0xffff, 0, 0x8080},
	}
	for _, rgb := range tests {
		value := rgbToANSI256(rgb[0], rgb[1], rgb[2])
		if value < 16 || value > 255 {
			t.Fatalf("rgbToANSI256(%v) = %d", rgb, value)
		}
	}
}

func TestEmptySerializers(t *testing.T) {
	if serialiseASCII(nil) != "" || serialiseColor256(nil) != "" || serialiseHalfBlock(nil) != "" {
		t.Fatal("empty cell grids should serialize to an empty string")
	}
}
