package timeline

import "testing"

// threeRanges keeps 2-5 s, 10-12 s and 20-26 s of a 30 s recording, so the
// trimmed audio is 11 s long with joints at 3 s and 5 s.
var threeRanges = Timeline{
	SourceDuration: 30,
	Kept:           []Range{{Start: 2, End: 5}, {Start: 10, End: 12}, {Start: 20, End: 26}},
}

func TestStartAndEndMapAcrossSeveralRanges(t *testing.T) {
	tests := []struct {
		name      string
		at        float64
		wantStart float64
		wantEnd   float64
	}{
		{"start of the trimmed audio", 0, 2, 2},
		{"inside the first range", 1.25, 3.25, 3.25},
		{"on the first joint", 3, 10, 5},
		{"inside the second range", 4.5, 11.5, 11.5},
		{"on the second joint", 5, 20, 12},
		{"inside the last range", 7.48, 22.48, 22.48},
		{"end of the trimmed audio", 11, 26, 26},
		{"past the end is clamped", 11.2, 26, 26},
		{"before the start is clamped", -0.5, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := threeRanges.Start(tt.at); got != tt.wantStart {
				t.Errorf("Start(%v) = %v, want %v", tt.at, got, tt.wantStart)
			}
			if got := threeRanges.End(tt.at); got != tt.wantEnd {
				t.Errorf("End(%v) = %v, want %v", tt.at, got, tt.wantEnd)
			}
		})
	}
}

func TestSpanPlacesEdgeWordsInOneRange(t *testing.T) {
	tests := []struct {
		name       string
		start, end float64
		wantStart  float64
		wantEnd    float64
	}{
		{"inside one range", 0.5, 0.9, 2.5, 2.9},
		{"ends exactly on a joint", 2.6, 3, 4.6, 5},
		{"starts exactly on a joint", 3, 3.4, 10, 10.4},
		{"mostly before a joint", 2.7, 3.1, 4.7, 5},
		{"mostly after a joint", 2.9, 3.5, 10, 10.5},
		{"covers a whole short range", 2.9, 5.2, 10, 12},
		{"zero length on a joint", 5, 5, 20, 20},
		{"runs past the end", 10.8, 11.3, 25.8, 26},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd := threeRanges.Span(tt.start, tt.end)
			if gotStart != tt.wantStart || gotEnd != tt.wantEnd {
				t.Errorf("Span(%v, %v) = (%v, %v), want (%v, %v)", tt.start, tt.end, gotStart, gotEnd, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestUntrimmedTimelineKeepsTimes(t *testing.T) {
	var identity Timeline
	if identity.Trimmed() {
		t.Fatal("the zero timeline reports trimming")
	}
	if got := identity.Start(1.0000004); got != 1 {
		t.Errorf("Start = %v, want 1", got)
	}
	if got := identity.End(3.25); got != 3.25 {
		t.Errorf("End = %v, want 3.25", got)
	}
	if start, end := identity.Span(0.1, 0.2); start != 0.1 || end != 0.2 {
		t.Errorf("Span = (%v, %v), want (0.1, 0.2)", start, end)
	}
}

func TestMappedTimesAreRounded(t *testing.T) {
	// 0.48 s into a range starting at 12.34 s, after an 3.2 s range: plain
	// float arithmetic gives 12.819999999999999.
	tl := Timeline{Kept: []Range{{Start: 0.1, End: 3.3}, {Start: 12.34, End: 20}}}
	if got := tl.Start(3.2 + 0.48); got != 12.82 {
		t.Errorf("Start = %v, want 12.82", got)
	}
}
