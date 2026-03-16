package tui

// Layout holds the computed dimensions for each pane.
type Layout struct {
	TotalW int
	TotalH int

	TopBarH    int
	BottomBarH int
	ContentH   int

	LeftW   int
	CenterW int
	RightW  int
}

const (
	minLeftW   = 20
	minRightW  = 22
	minCenterW = 28
	topBarH    = 1
	bottomBarH = 1
)

// Compute derives pane dimensions from total terminal size.
func Compute(totalW, totalH int) Layout {
	if totalW < 1 {
		totalW = 80
	}
	if totalH < 1 {
		totalH = 24
	}

	contentH := totalH - topBarH - bottomBarH
	if contentH < 4 {
		contentH = 4
	}

	leftW := maxInt(minLeftW, totalW*22/100)
	rightW := maxInt(minRightW, totalW*22/100)
	centerW := totalW - leftW - rightW
	if centerW < minCenterW {
		excess := minCenterW - centerW
		leftW -= excess / 2
		rightW -= excess - excess/2
		centerW = minCenterW
	}

	return Layout{
		TotalW:     totalW,
		TotalH:     totalH,
		TopBarH:    topBarH,
		BottomBarH: bottomBarH,
		ContentH:   contentH,
		LeftW:      leftW,
		CenterW:    centerW,
		RightW:     rightW,
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
