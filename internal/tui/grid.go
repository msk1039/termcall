package tui

// computeGrid calculates the cell bounding box and inner ASCII frame size.
func computeGrid(peerCount, termW, termH int) (cols, boxW, boxH, innerW, innerH int) {
	if peerCount <= 0 {
		return 1, termW, termH, 40, 15
	}

	bestCols := 1
	bestRows := peerCount
	bestScore := -1

	for c := 1; c <= peerCount; c++ {
		r := (peerCount + c - 1) / c

		bW := termW / c
		bH := termH / r

		maxIW := bW - 2
		maxIH := bH - 3 // marginV(2) + topbar(1)

		if maxIW < 10 {
			maxIW = 10
		}
		if maxIH < 5 {
			maxIH = 5
		}

		score := maxIW * maxIH

		// Heavy penalty if we can't fit the canonical maxSendWidth
		if maxIW >= maxSendWidth {
			score += 1000000
		} else {
			// If we must squash, heavily prioritize wider layouts to minimize cutoff
			score += maxIW * 1000
		}

		if score > bestScore {
			bestScore = score
			bestCols = c
			bestRows = r
		}
	}

	cols = bestCols
	rows := bestRows

	// Calculate outer bounding box for each cell
	boxW = termW / cols
	boxH = termH / rows

	// We want 1 unit of margin on all sides of a cell.
	// We also don't use lipgloss Border anymore. The top bar takes 1 line.
	// So overhead per cell:
	// Margin: 2 horiz, 2 vert
	// TopBar: 1 vert
	marginH := 2
	marginV := 2

	maxInnerW := boxW - marginH
	maxInnerH := boxH - marginV - 1 // 1 for the top bar

	if maxInnerW < 10 {
		maxInnerW = 10
	}
	if maxInnerH < 5 {
		maxInnerH = 5
	}

	// We no longer constrain targetW and targetH by aspect ratio.
	// The UpscaleCells function will crop-to-fit (cover) the video to fill
	// this entire space, maximizing screen usage.
	targetW := maxInnerW
	targetH := maxInnerH

	return cols, boxW, boxH, targetW, targetH
}
