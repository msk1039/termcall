package ascii

// Cell represents one rendered terminal cell's colour data.
type Cell struct {
	Char byte // gradient character (ASCII/Color256) or 0 for HalfBlock
	Fg   int  // foreground ANSI-256 colour index (-1 if unused)
	Bg   int  // background ANSI-256 colour index (-1 if unused)
}

// Stabiliser suppresses per-frame camera noise by ignoring colour changes
// smaller than a configurable threshold. It operates on ANSI-256 colour
// indices (integers), keeping a running baseline of the previous frame so
// static areas stop flickering between adjacent palette entries.
type Stabiliser struct {
	prev      [][]Cell
	threshold int
	w, h      int
}

// NewStabiliser returns a Stabiliser that suppresses ANSI-index changes up to
// the given threshold.
func NewStabiliser(threshold int) *Stabiliser {
	return &Stabiliser{threshold: threshold}
}

// Reset clears the baseline so the next frame is accepted as-is. Call this on
// render mode changes.
func (s *Stabiliser) Reset() {
	s.prev = nil
	s.w, s.h = 0, 0
}

// Stabilise compares cells with the previous frame. For each cell, if the
// colour change is within the threshold and the gradient char is unchanged,
// the old value is kept (noise suppressed); otherwise the new value is
// accepted (real motion). The result is stored as the new baseline.
//
// On the first frame or a dimension change, the grid is accepted as-is.
func (s *Stabiliser) Stabilise(cells [][]Cell) [][]Cell {
	h := len(cells)
	w := 0
	if h > 0 {
		w = len(cells[0])
	}

	if s.prev == nil || s.w != w || s.h != h {
		s.prev = deepCopyCells(cells)
		s.w, s.h = w, h
		return s.prev
	}

	out := make([][]Cell, h)
	for y := 0; y < h; y++ {
		row := make([]Cell, w)
		for x := 0; x < w; x++ {
			n := cells[y][x]
			o := s.prev[y][x]
			if abs(n.Fg-o.Fg) <= s.threshold &&
				abs(n.Bg-o.Bg) <= s.threshold &&
				n.Char == o.Char {
				row[x] = o // suppress: keep previous value
			} else {
				row[x] = n // accept: real motion
			}
		}
		out[y] = row
	}
	s.prev = out
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// deepCopyCells returns a fully-independent copy of a cell grid.
func deepCopyCells(cells [][]Cell) [][]Cell {
	out := make([][]Cell, len(cells))
	for y := range cells {
		out[y] = make([]Cell, len(cells[y]))
		copy(out[y], cells[y])
	}
	return out
}
