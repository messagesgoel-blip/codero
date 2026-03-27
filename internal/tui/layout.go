package tui

// Layout holds the computed dimensions for each pane.
type Layout struct {
	TotalW int
	TotalH int

	TopBarH    int
	BottomBarH int
	ContentH   int

	LeftW     int
	CenterW   int
	PipelineW int
	RightW    int
}

const (
	minLeftW     = 26
	minCenterW   = 40
	minPipelineW = 16
	minRightW    = 24
	topBarH      = 1
	bottomBarH   = 3
)

// Compute derives pane dimensions from total terminal size.
func Compute(totalW, totalH int) Layout {
	if totalW < 1 {
		totalW = 80
	}
	if totalH < 1 {
		totalH = 24
	}

	topBarH, bottomBarH := topBarH, bottomBarH
	if topBarH+bottomBarH > totalH {
		overflow := topBarH + bottomBarH - totalH
		if bottomBarH >= overflow {
			bottomBarH -= overflow
		} else {
			overflow -= bottomBarH
			bottomBarH = 0
			topBarH = maxInt(0, topBarH-overflow)
		}
	}
	contentH := totalH - topBarH - bottomBarH
	if contentH < 0 {
		contentH = 0
	}

	var leftW, centerW, pipelineW, rightW int
	minSum := minLeftW + minCenterW + minPipelineW + minRightW
	if totalW >= minSum {
		leftW = minLeftW
		centerW = minCenterW
		pipelineW = minPipelineW
		rightW = minRightW

		extra := totalW - minSum
		adds := []int{
			extra * 20 / 100,
			extra * 40 / 100,
			extra * 15 / 100,
			extra * 25 / 100,
		}
		leftW += adds[0]
		centerW += adds[1]
		pipelineW += adds[2]
		rightW += adds[3]

		used := adds[0] + adds[1] + adds[2] + adds[3]
		for i := 0; i < extra-used; i++ {
			switch i % 4 {
			case 0:
				centerW++
			case 1:
				leftW++
			case 2:
				rightW++
			default:
				pipelineW++
			}
		}
	} else {
		if totalW < paneCount {
			centerW = minInt(1, totalW)
			remaining := totalW - centerW
			leftW = minInt(1, maxInt(0, remaining))
			remaining -= leftW
			rightW = minInt(1, maxInt(0, remaining))
			remaining -= rightW
			pipelineW = maxInt(0, remaining)
		} else {
			weights := []int{20, 40, 15, 25}
			leftW = maxInt(1, totalW*weights[0]/100)
			centerW = maxInt(1, totalW*weights[1]/100)
			pipelineW = maxInt(1, totalW*weights[2]/100)
			rightW = totalW - leftW - centerW - pipelineW
			if rightW < 1 {
				rightW = 1
			}

			used := leftW + centerW + pipelineW + rightW
			if used != totalW {
				diff := used - totalW
				for diff > 0 {
					reduced := false
					if centerW > leftW && centerW > pipelineW && centerW > 1 {
						centerW--
						reduced = true
					} else if leftW >= pipelineW && leftW > 1 {
						leftW--
						reduced = true
					} else if pipelineW > 1 {
						pipelineW--
						reduced = true
					} else if rightW > 1 {
						rightW--
						reduced = true
					}
					if !reduced {
						break
					}
					diff--
				}
				for diff < 0 {
					switch (-diff) % 4 {
					case 0:
						centerW++
					case 1:
						leftW++
					case 2:
						rightW++
					default:
						pipelineW++
					}
					diff++
				}
			}
		}
	}

	return Layout{
		TotalW:     totalW,
		TotalH:     totalH,
		TopBarH:    topBarH,
		BottomBarH: bottomBarH,
		ContentH:   contentH,
		LeftW:      leftW,
		CenterW:    centerW,
		PipelineW:  pipelineW,
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
