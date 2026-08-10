package ascii

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	MaxFrameWidth        = 100
	MaxFrameHeight       = 30
	MaxFrameCells        = MaxFrameWidth * MaxFrameHeight
	MaxEncodedFrameBytes = 128 * 1024
)

var (
	ErrFrameTooLarge     = errors.New("frame exceeds protocol limits")
	ErrMalformedFrame    = errors.New("malformed frame")
	ErrUnsupportedFrame  = errors.New("unsupported frame type")
	ErrMissingKeyframe   = errors.New("delta has no matching keyframe")
	ErrInvalidFrameValue = errors.New("frame contains an invalid value")
)

// DeltaEncoder tracks the previous frame and emits either a complete keyframe
// or the changed cells. Encoded frames are data, never terminal escape text.
type DeltaEncoder struct {
	prev          [][]Cell
	w             int
	h             int
	mode          RenderMode
	frameCount    int
	keyframeEvery int
}

func NewDeltaEncoder(mode RenderMode, keyframeInterval int) *DeltaEncoder {
	if keyframeInterval < 1 {
		keyframeInterval = 30
	}
	return &DeltaEncoder{
		mode:          mode,
		keyframeEvery: keyframeInterval,
	}
}

func (e *DeltaEncoder) Reset() {
	e.prev = nil
	e.w = 0
	e.h = 0
	e.frameCount = 0
}

func (e *DeltaEncoder) Encode(cells [][]Cell) string {
	w, h, err := validateGrid(cells, e.mode)
	if err != nil {
		return ""
	}

	e.frameCount++
	requiresKeyframe := e.prev == nil || e.w != w || e.h != h || e.frameCount >= e.keyframeEvery
	if requiresKeyframe {
		e.frameCount = 0
		e.prev = deepCopyCells(cells)
		e.w = w
		e.h = h
		return e.encodeKeyframe(cells)
	}

	encoded := e.encodeDelta(cells)
	if encoded == "" {
		e.frameCount = 0
		e.prev = deepCopyCells(cells)
		return e.encodeKeyframe(cells)
	}
	e.prev = deepCopyCells(cells)
	return encoded
}

func (e *DeltaEncoder) encodeKeyframe(cells [][]Cell) string {
	h := len(cells)
	w := len(cells[0])

	var builder strings.Builder
	fmt.Fprintf(&builder, "K %d %d %d\n", w, h, int(e.mode))
	for _, row := range cells {
		for x, cell := range row {
			if x > 0 {
				builder.WriteByte(';')
			}
			fmt.Fprintf(&builder, "%d,%d,%d", cell.Fg, cell.Bg, int(cell.Char))
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

// encodeDelta returns an empty string when a keyframe is more appropriate.
func (e *DeltaEncoder) encodeDelta(cells [][]Cell) string {
	h := len(cells)
	w := len(cells[0])
	total := w * h

	var changes strings.Builder
	count := 0
	for y, row := range cells {
		for x, cell := range row {
			if cell == e.prev[y][x] {
				continue
			}
			fmt.Fprintf(&changes, "%d %d %d %d %d\n", y, x, cell.Fg, cell.Bg, int(cell.Char))
			count++
		}
	}
	if count > total/2 {
		return ""
	}
	return fmt.Sprintf("D %d %d %d\n%s", w, h, count, changes.String())
}

type DeltaDecoder struct {
	grid [][]Cell
	w    int
	h    int
	mode RenderMode
	has  bool
}

func NewDeltaDecoder() *DeltaDecoder {
	return &DeltaDecoder{
		grid: [][]Cell{},
	}
}

func (d *DeltaDecoder) Decode(data string) (string, error) {
	cells, mode, err := d.DecodeCells(data)
	if err != nil {
		return "", err
	}
	return SerialiseMode(cells, mode), nil
}

func (d *DeltaDecoder) DecodeCells(data string) ([][]Cell, RenderMode, error) {
	if len(data) == 0 {
		return nil, 0, fmt.Errorf("%w: empty payload", ErrMalformedFrame)
	}
	if len(data) > MaxEncodedFrameBytes {
		return nil, 0, ErrFrameTooLarge
	}

	var err error
	switch data[0] {
	case 'K':
		err = d.decodeKeyframe(data)
	case 'D':
		err = d.decodeDelta(data)
	default:
		err = ErrUnsupportedFrame
	}
	if err != nil {
		return nil, 0, err
	}
	return deepCopyCells(d.grid), d.mode, nil
}

func (d *DeltaDecoder) decodeKeyframe(data string) error {
	header, body, err := splitFrame(data)
	if err != nil {
		return err
	}
	fields := strings.Fields(header)
	if len(fields) != 4 || fields[0] != "K" {
		return ErrMalformedFrame
	}

	w, err := parseInt(fields[1])
	if err != nil {
		return err
	}
	h, err := parseInt(fields[2])
	if err != nil {
		return err
	}
	modeValue, err := parseInt(fields[3])
	if err != nil {
		return err
	}
	mode := RenderMode(modeValue)
	if err := validateDimensions(w, h); err != nil {
		return err
	}
	if !validMode(mode) {
		return fmt.Errorf("%w: render mode %d", ErrInvalidFrameValue, modeValue)
	}

	lines := payloadLines(body)
	if len(lines) != h {
		return fmt.Errorf("%w: got %d rows, want %d", ErrMalformedFrame, len(lines), h)
	}

	grid := make([][]Cell, h)
	for y, line := range lines {
		encodedCells := strings.Split(line, ";")
		if len(encodedCells) != w {
			return fmt.Errorf("%w: row %d has %d cells, want %d", ErrMalformedFrame, y, len(encodedCells), w)
		}

		row := make([]Cell, w)
		for x, encodedCell := range encodedCells {
			cell, err := parseCell(encodedCell, mode)
			if err != nil {
				return fmt.Errorf("row %d cell %d: %w", y, x, err)
			}
			row[x] = cell
		}
		grid[y] = row
	}

	d.grid = grid
	d.w = w
	d.h = h
	d.mode = mode
	d.has = true
	return nil
}

func (d *DeltaDecoder) decodeDelta(data string) error {
	header, body, err := splitFrame(data)
	if err != nil {
		return err
	}
	fields := strings.Fields(header)
	if len(fields) != 4 || fields[0] != "D" {
		return ErrMalformedFrame
	}

	w, err := parseInt(fields[1])
	if err != nil {
		return err
	}
	h, err := parseInt(fields[2])
	if err != nil {
		return err
	}
	count, err := parseInt(fields[3])
	if err != nil {
		return err
	}
	if err := validateDimensions(w, h); err != nil {
		return err
	}
	if count < 0 || count > w*h {
		return fmt.Errorf("%w: change count %d", ErrInvalidFrameValue, count)
	}
	if !d.has || d.w != w || d.h != h || len(d.grid) == 0 {
		return ErrMissingKeyframe
	}

	lines := payloadLines(body)
	if count == 0 && len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	if len(lines) != count {
		return fmt.Errorf("%w: got %d changes, want %d", ErrMalformedFrame, len(lines), count)
	}

	grid := deepCopyCells(d.grid)
	seen := make(map[int]struct{}, count)
	for i, line := range lines {
		change := strings.Fields(line)
		if len(change) != 5 {
			return fmt.Errorf("%w: change %d has %d fields", ErrMalformedFrame, i, len(change))
		}

		row, err := parseInt(change[0])
		if err != nil {
			return err
		}
		col, err := parseInt(change[1])
		if err != nil {
			return err
		}
		if row < 0 || row >= h || col < 0 || col >= w {
			return fmt.Errorf("%w: cell position %d,%d", ErrInvalidFrameValue, row, col)
		}
		index := row*w + col
		if _, exists := seen[index]; exists {
			return fmt.Errorf("%w: duplicate cell position %d,%d", ErrMalformedFrame, row, col)
		}
		seen[index] = struct{}{}

		cell, err := parseCellFields(change[2], change[3], change[4], d.mode)
		if err != nil {
			return fmt.Errorf("change %d: %w", i, err)
		}
		grid[row][col] = cell
	}

	d.grid = grid
	return nil
}

func splitFrame(data string) (string, string, error) {
	newline := strings.IndexByte(data, '\n')
	if newline < 0 {
		return "", "", ErrMalformedFrame
	}
	return data[:newline], data[newline+1:], nil
}

func payloadLines(body string) []string {
	body = strings.TrimSuffix(body, "\n")
	return strings.Split(body, "\n")
}

func parseCell(encoded string, mode RenderMode) (Cell, error) {
	fields := strings.Split(encoded, ",")
	if len(fields) != 3 {
		return Cell{}, ErrMalformedFrame
	}
	return parseCellFields(fields[0], fields[1], fields[2], mode)
}

func parseCellFields(fgValue, bgValue, charValue string, mode RenderMode) (Cell, error) {
	fg, err := parseInt(fgValue)
	if err != nil {
		return Cell{}, err
	}
	bg, err := parseInt(bgValue)
	if err != nil {
		return Cell{}, err
	}
	charInt, err := parseInt(charValue)
	if err != nil {
		return Cell{}, err
	}
	if fg < -1 || fg > 255 || bg < -1 || bg > 255 {
		return Cell{}, ErrInvalidFrameValue
	}
	if charInt < 0 || charInt > 126 {
		return Cell{}, ErrInvalidFrameValue
	}
	if charInt != 0 && charInt < 32 {
		return Cell{}, ErrInvalidFrameValue
	}
	if charInt == 0 && mode != ModeHalfBlock {
		return Cell{}, ErrInvalidFrameValue
	}
	return Cell{
		Char: byte(charInt),
		Fg:   fg,
		Bg:   bg,
	}, nil
}

func parseInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, ErrMalformedFrame
	}
	return parsed, nil
}

func validateGrid(cells [][]Cell, mode RenderMode) (int, int, error) {
	if !validMode(mode) {
		return 0, 0, ErrInvalidFrameValue
	}
	h := len(cells)
	if h == 0 {
		return 0, 0, ErrInvalidFrameValue
	}
	w := len(cells[0])
	if err := validateDimensions(w, h); err != nil {
		return 0, 0, err
	}
	for _, row := range cells {
		if len(row) != w {
			return 0, 0, ErrMalformedFrame
		}
		for _, cell := range row {
			if _, err := parseCellFields(
				strconv.Itoa(cell.Fg),
				strconv.Itoa(cell.Bg),
				strconv.Itoa(int(cell.Char)),
				mode,
			); err != nil {
				return 0, 0, err
			}
		}
	}
	return w, h, nil
}

func validateDimensions(w, h int) error {
	isPositive := w > 0 && h > 0
	isWithinBounds := w <= MaxFrameWidth && h <= MaxFrameHeight
	if !isPositive || !isWithinBounds || w*h > MaxFrameCells {
		return ErrFrameTooLarge
	}
	return nil
}

func validMode(mode RenderMode) bool {
	return mode >= ModeASCII && mode <= ModeHalfBlock
}

// StatusFrame returns a small, safe keyframe used for local media state such
// as a disabled camera. Only printable ASCII is emitted.
func StatusFrame(message string) string {
	printable := make([]byte, 0, len(message))
	for i := 0; i < len(message) && len(printable) < 40; i++ {
		if message[i] >= 32 && message[i] <= 126 {
			printable = append(printable, message[i])
		}
	}
	if len(printable) == 0 {
		printable = []byte("Unavailable")
	}
	width := len(printable) + 4
	if width < 16 {
		width = 16
	}
	grid := make([][]Cell, 3)
	for y := range grid {
		grid[y] = make([]Cell, width)
		for x := range grid[y] {
			grid[y][x] = Cell{Char: ' ', Fg: -1, Bg: -1}
		}
	}
	start := (width - len(printable)) / 2
	for index, char := range printable {
		grid[1][start+index].Char = char
	}
	return NewDeltaEncoder(ModeASCII, 1).Encode(grid)
}
