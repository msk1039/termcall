package ascii

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Wire format
//
//	Keyframe: "K <w> <h> <mode>\n" + rows of ';'-separated cells
//	           ("<fg>,<bg>,<char>"), rows separated by '\n'.
//	Delta:     "D <w> <h> <count>\n" + one line per changed cell
//	           ("<row> <col> <fg> <bg> <char>").
//
// Any other payload is treated as a legacy plain-ANSI string (returned as-is)
// so new clients interoperate with old ones.

// DeltaEncoder tracks the previous frame's cell grid and emits either a
// keyframe (full grid) or a delta (only changed cells). A keyframe is forced
// on the first frame, on a dimension change, or every keyframeEvery frames.
type DeltaEncoder struct {
	prev          [][]Cell
	w, h          int
	mode          RenderMode
	frameCount    int
	keyframeEvery int
}

func NewDeltaEncoder(mode RenderMode, keyframeInterval int) *DeltaEncoder {
	return &DeltaEncoder{mode: mode, keyframeEvery: keyframeInterval}
}

// Reset clears the baseline so the next frame is a keyframe.
func (e *DeltaEncoder) Reset() {
	e.prev = nil
	e.w, e.h = 0, 0
	e.frameCount = 0
}

// Encode returns the wire string for cells: a keyframe ("K ...") or delta
// ("D ..."). If a delta would change more than half the grid, a keyframe is
// sent instead (smaller and lets the receiver resync).
func (e *DeltaEncoder) Encode(cells [][]Cell) string {
	e.frameCount++
	h := len(cells)
	w := 0
	if h > 0 {
		w = len(cells[0])
	}

	if e.prev == nil || e.w != w || e.h != h || e.frameCount >= e.keyframeEvery {
		e.frameCount = 0
		e.prev = deepCopyCells(cells)
		e.w, e.h = w, h
		return e.encodeKeyframe(cells)
	}

	enc := e.encodeDelta(cells)
	if enc == "" {
		// Delta too large — send a keyframe instead.
		e.frameCount = 0
		e.prev = deepCopyCells(cells)
		return e.encodeKeyframe(cells)
	}
	e.prev = deepCopyCells(cells)
	return enc
}

func (e *DeltaEncoder) encodeKeyframe(cells [][]Cell) string {
	h := len(cells)
	w := 0
	if h > 0 {
		w = len(cells[0])
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "K %d %d %d\n", w, h, int(e.mode))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x > 0 {
				sb.WriteByte(';')
			}
			c := cells[y][x]
			fmt.Fprintf(&sb, "%d,%d,%d", c.Fg, c.Bg, int(c.Char))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// encodeDelta returns the delta string, or "" if more than half the grid
// changed (caller should send a keyframe instead).
func (e *DeltaEncoder) encodeDelta(cells [][]Cell) string {
	h := len(cells)
	w := 0
	if h > 0 {
		w = len(cells[0])
	}
	total := w * h
	if total == 0 {
		return ""
	}

	var sb strings.Builder
	count := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y][x]
			p := e.prev[y][x]
			if c != p {
				fmt.Fprintf(&sb, "%d %d %d %d %d\n", y, x, c.Fg, c.Bg, int(c.Char))
				count++
			}
		}
	}
	if count > total/2 {
		return ""
	}
	return fmt.Sprintf("D %d %d %d\n%s", w, h, count, sb.String())
}

// DeltaDecoder receives keyframes and deltas, maintains a cell grid, and
// produces full ANSI strings for display. Plain (non-K/D) payloads are
// returned unchanged for backward compatibility.
type DeltaDecoder struct {
	grid [][]Cell
	w, h int
	mode RenderMode
	has  bool
}

func NewDeltaDecoder() *DeltaDecoder {
	return &DeltaDecoder{}
}

// Decode parses a wire string (keyframe, delta, or legacy plain ANSI) and
// returns the full ANSI display string.
func (d *DeltaDecoder) Decode(data string) string {
	if data == "" {
		return ""
	}
	switch data[0] {
	case 'K':
		return d.decodeKeyframe(data)
	case 'D':
		return d.decodeDelta(data)
	default:
		return data // legacy plain ANSI string
	}
}

func (d *DeltaDecoder) decodeKeyframe(data string) string {
	nl := strings.IndexByte(data, '\n')
	if nl < 0 {
		return data
	}
	parts := strings.Fields(data[:nl])
	if len(parts) < 4 {
		return data
	}
	w, err1 := strconv.Atoi(parts[1])
	h, err2 := strconv.Atoi(parts[2])
	mode, err3 := strconv.Atoi(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || w <= 0 || h <= 0 {
		return data
	}

	d.w, d.h, d.mode, d.has = w, h, RenderMode(mode), true
	grid := make([][]Cell, h)
	for y := range grid {
		grid[y] = make([]Cell, w)
	}
	r := bufio.NewReader(strings.NewReader(data[nl+1:]))
	for y := 0; y < h; y++ {
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			break
		}
		line = strings.TrimRight(line, "\n")
		cells := strings.Split(line, ";")
		for x := 0; x < w && x < len(cells); x++ {
			grid[y][x] = parseCell(cells[x])
		}
	}
	d.grid = grid
	return d.serialise()
}

func (d *DeltaDecoder) decodeDelta(data string) string {
	nl := strings.IndexByte(data, '\n')
	if nl < 0 {
		return data
	}
	parts := strings.Fields(data[:nl])
	if len(parts) < 4 {
		return data
	}
	w, err1 := strconv.Atoi(parts[1])
	h, err2 := strconv.Atoi(parts[2])
	count, err3 := strconv.Atoi(parts[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return data
	}
	if !d.has || d.w != w || d.h != h || d.grid == nil {
		// Delta without a matching keyframe — cannot apply safely.
		return ""
	}
	r := bufio.NewReader(strings.NewReader(data[nl+1:]))
	for i := 0; i < count; i++ {
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			break
		}
		f := strings.Fields(strings.TrimRight(line, "\n"))
		if len(f) < 5 {
			continue
		}
		row, e1 := strconv.Atoi(f[0])
		col, e2 := strconv.Atoi(f[1])
		fg, e3 := strconv.Atoi(f[2])
		bg, e4 := strconv.Atoi(f[3])
		ch, e5 := strconv.Atoi(f[4])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
			continue
		}
		if row < 0 || row >= h || col < 0 || col >= w {
			continue
		}
		d.grid[row][col] = Cell{Char: byte(ch), Fg: fg, Bg: bg}
	}
	return d.serialise()
}

func (d *DeltaDecoder) serialise() string {
	if d.grid == nil {
		return ""
	}
	switch d.mode {
	case ModeASCII:
		return serialiseASCII(d.grid)
	case ModeColor256:
		return serialiseColor256(d.grid)
	case ModeHalfBlock:
		return serialiseHalfBlock(d.grid)
	}
	return ""
}

// parseCell parses "<fg>,<bg>,<char>" into a Cell. Invalid input yields a
// default cell with Fg/Bg = -1.
func parseCell(s string) Cell {
	parts := strings.Split(s, ",")
	if len(parts) < 3 {
		return Cell{Fg: -1, Bg: -1}
	}
	fg, e1 := strconv.Atoi(parts[0])
	bg, e2 := strconv.Atoi(parts[1])
	ch, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return Cell{Fg: -1, Bg: -1}
	}
	return Cell{Char: byte(ch), Fg: fg, Bg: bg}
}
