package main

import "testing"

func TestClosestLineIndex(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		line     string
		previous int
		want     int
	}{
		{name: "shifted line", lines: []string{"new", "selected", "later"}, line: "selected", previous: 0, want: 1},
		{name: "nearest duplicate", lines: []string{"selected", "middle", "selected"}, line: "selected", previous: 2, want: 2},
		{name: "missing line", lines: []string{"new", "later"}, line: "selected", previous: 1, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := closestLineIndex(tt.lines, tt.line, tt.previous); got != tt.want {
				t.Fatalf("closestLineIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}
