package tui_test

import (
	"testing"

	"github.com/codero/codero/internal/tui"
)

func TestCompute_Standard(t *testing.T) {
	l := tui.Compute(80, 24)
	if l.TotalW != 80 {
		t.Errorf("TotalW: want 80, got %d", l.TotalW)
	}
	if l.TotalH != 24 {
		t.Errorf("TotalH: want 24, got %d", l.TotalH)
	}
	if l.LeftW+l.CenterW+l.PipelineW+l.RightW != l.TotalW {
		t.Errorf("pane widths %d+%d+%d+%d != %d", l.LeftW, l.CenterW, l.PipelineW, l.RightW, l.TotalW)
	}
	if l.ContentH != l.TotalH-l.TopBarH-l.BottomBarH {
		t.Errorf("ContentH mismatch: %d vs %d", l.ContentH, l.TotalH-l.TopBarH-l.BottomBarH)
	}
}

func TestCompute_Wide(t *testing.T) {
	l := tui.Compute(160, 50)
	if l.LeftW+l.CenterW+l.PipelineW+l.RightW != l.TotalW {
		t.Errorf("pane widths %d+%d+%d+%d != %d", l.LeftW, l.CenterW, l.PipelineW, l.RightW, l.TotalW)
	}
}

func TestCompute_MinimumWidths(t *testing.T) {
	l := tui.Compute(160, 50)
	if l.CenterW < 34 {
		t.Errorf("CenterW %d < minCenterW 34", l.CenterW)
	}
	if l.LeftW < 24 {
		t.Errorf("LeftW %d < minLeftW 24", l.LeftW)
	}
	if l.PipelineW < 18 {
		t.Errorf("PipelineW %d < minPipelineW 18", l.PipelineW)
	}
	if l.RightW < 22 {
		t.Errorf("RightW %d < minRightW 22", l.RightW)
	}
}

func TestCompute_Narrow(t *testing.T) {
	l := tui.Compute(50, 24)
	if l.LeftW < 1 {
		t.Errorf("LeftW: expected >=1, got %d", l.LeftW)
	}
	if l.CenterW < 1 {
		t.Errorf("CenterW: expected >=1, got %d", l.CenterW)
	}
	if l.PipelineW < 1 {
		t.Errorf("PipelineW: expected >=1, got %d", l.PipelineW)
	}
	if l.RightW < 1 {
		t.Errorf("RightW: expected >=1, got %d", l.RightW)
	}
	if l.LeftW+l.CenterW+l.PipelineW+l.RightW != l.TotalW {
		t.Errorf("pane widths %d+%d+%d+%d != %d", l.LeftW, l.CenterW, l.PipelineW, l.RightW, l.TotalW)
	}
}

func TestCompute_Zero(t *testing.T) {
	l := tui.Compute(0, 0)
	if l.TotalW < 1 {
		t.Errorf("expected TotalW >= 1 for zero input, got %d", l.TotalW)
	}
}
