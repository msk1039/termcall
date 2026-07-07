package ascii

// UpscaleCells scales a cell grid to fit exactly targetW x targetH.
// It uses nearest-neighbor sampling, which naturally upscales by repeating
// characters/blocks, and downscales by skipping them.
// This preserves the aspect ratio determined by the caller (targetW/targetH).
func UpscaleCells(cells [][]Cell, targetW, targetH int) [][]Cell {
	if len(cells) == 0 || targetW <= 0 || targetH <= 0 {
		return cells
	}

	srcH := len(cells)
	srcW := len(cells[0])
	if srcW == 0 {
		return cells
	}

	out := make([][]Cell, targetH)
	for y := 0; y < targetH; y++ {
		srcY := y * srcH / targetH
		if srcY >= srcH {
			srcY = srcH - 1
		}
		
		row := make([]Cell, targetW)
		srcRow := cells[srcY]
		for x := 0; x < targetW; x++ {
			srcX := x * srcW / targetW
			if srcX >= srcW {
				srcX = srcW - 1
			}
			row[x] = srcRow[srcX]
		}
		out[y] = row
	}
	return out
}
