package ascii

import "testing"

func mkHalfBlockGrid(w, h, base int) [][]Cell {
	cells := make([][]Cell, h)
	for y := range cells {
		row := make([]Cell, w)
		for x := range row {
			row[x] = Cell{Char: 0, Fg: base + (y*w+x)%10, Bg: base + 100}
		}
		cells[y] = row
	}
	return cells
}

func cellsEqual(a, b [][]Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for y := range a {
		if len(a[y]) != len(b[y]) {
			return false
		}
		for x := range a[y] {
			if a[y][x] != b[y][x] {
				return false
			}
		}
	}
	return true
}

func TestDeltaRoundTrip(t *testing.T) {
	enc := NewDeltaEncoder(ModeHalfBlock, 30)
	dec := NewDeltaDecoder()

	g1 := mkHalfBlockGrid(4, 3, 60)
	out1 := enc.Encode(g1)
	if len(out1) == 0 || out1[0] != 'K' {
		t.Fatalf("first frame should be a keyframe, got %q", out1)
	}
	want1 := serialiseHalfBlock(g1)
	if got := dec.Decode(out1); got != want1 {
		t.Errorf("keyframe round-trip mismatch:\ngot:  %q\nwant: %q", got, want1)
	}

	// Unchanged frame → delta with count 0, decodes to the same display.
	out2 := enc.Encode(g1)
	if out2[0] != 'D' {
		t.Fatalf("unchanged frame should be a delta, got %q", out2)
	}
	if got := dec.Decode(out2); got != want1 {
		t.Errorf("empty delta round-trip mismatch:\ngot:  %q\nwant: %q", got, want1)
	}

	// Change one cell → delta with count 1.
	g2 := deepCopyCells(g1)
	g2[1][2] = Cell{Char: 0, Fg: 200, Bg: 50}
	out3 := enc.Encode(g2)
	if out3[0] != 'D' {
		t.Fatalf("one-cell change should be a delta, got %q", out3)
	}
	want3 := serialiseHalfBlock(g2)
	if got := dec.Decode(out3); got != want3 {
		t.Errorf("delta round-trip mismatch:\ngot:  %q\nwant: %q", got, want3)
	}

	// Legacy plain-ANSI string → returned unchanged.
	legacy := "\x1b[48;5;16m▄\x1b[0m"
	if got := dec.Decode(legacy); got != legacy {
		t.Errorf("legacy passthrough mismatch: got %q", got)
	}
}

func TestDeltaKeyframeFallback(t *testing.T) {
	enc := NewDeltaEncoder(ModeHalfBlock, 1000) // large interval so we only test the >50% rule
	dec := NewDeltaDecoder()

	g1 := mkHalfBlockGrid(4, 2, 60)
	dec.Decode(enc.Encode(g1)) // seed keyframe

	// Change >50% of cells → encoder must fall back to a keyframe.
	g2 := deepCopyCells(g1)
	for y := range g2 {
		for x := range g2[y] {
			g2[y][x] = Cell{Char: 0, Fg: 5, Bg: 5}
		}
	}
	out := enc.Encode(g2)
	if out[0] != 'K' {
		t.Fatalf("large change should fall back to keyframe, got %q", out)
	}
	if got := dec.Decode(out); got != serialiseHalfBlock(g2) {
		t.Errorf("fallback keyframe round-trip mismatch")
	}
}

func TestStabiliser(t *testing.T) {
	s := NewStabiliser(1)
	g := mkHalfBlockGrid(3, 2, 60)

	out1 := s.Stabilise(g) // first frame → accept as-is

	out2 := s.Stabilise(g) // identical → fully suppressed
	if !cellsEqual(out2, out1) {
		t.Errorf("identical frame should be fully suppressed")
	}

	// ±1 Fg change → suppressed.
	g3 := deepCopyCells(g)
	g3[0][0] = Cell{Char: 0, Fg: g[0][0].Fg + 1, Bg: g[0][0].Bg}
	out3 := s.Stabilise(g3)
	if out3[0][0].Fg != out1[0][0].Fg {
		t.Errorf("±1 change should be suppressed")
	}

	// Large change → accepted.
	g4 := deepCopyCells(g)
	g4[0][0] = Cell{Char: 0, Fg: g[0][0].Fg + 10, Bg: g[0][0].Bg}
	out4 := s.Stabilise(g4)
	if out4[0][0].Fg != g4[0][0].Fg {
		t.Errorf("large change should be accepted")
	}
}
