package main

import (
	"github.com/awesome-gocui/gocui"
	"github.com/manojkiraneda/lazydebugger/v2/parsers/pldm"
	"strings"
	"testing"
)

func TestPldmRefreshPreservesSelectionAndManualPause(t *testing.T) {
	app := &App{
		testMode:         true,
		filteredLogLines: []string{"old", "selected", "newer"},
		logScrollPos:     1,
		autoScroll:       false,
	}
	app.publishPreparedPldmLines([]string{"earlier", "old", "selected", "newer", "latest"}, false)
	if app.logScrollPos != 2 || app.selectedLogLine != 0 {
		t.Fatalf("selection moved away from selected message: %d + %d", app.logScrollPos, app.selectedLogLine)
	}
	if app.autoScroll {
		t.Fatal("background refresh re-enabled auto-scroll after manual pause")
	}
	app.publishPreparedPldmLines([]string{"earlier", "old", "selected", "newer", "latest"}, false)
	if app.logScrollPos != 2 || app.autoScroll {
		t.Fatal("repeated refresh changed the selected message or manual pause")
	}
	app.publishPreparedPldmLines([]string{"different source"}, true)
	if app.logScrollPos != 0 || !app.autoScroll {
		t.Fatal("new journal selection did not reset the view")
	}
}

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

func TestReadFileByTypePanelShowsTypeFromUnfilteredRequest(t *testing.T) {
	gui, err := gocui.NewGui(gocui.OutputSimulator, true)
	if err != nil {
		t.Fatal(err)
	}
	defer gui.Close()
	view, err := gui.SetView("docker", 0, 0, 100, 40, 0)
	if err != nil && err != gocui.ErrUnknownView {
		t.Fatal(err)
	}
	request := "2026-10-26T02:47:40Z pldmd Tx: 80 3F 0B 03 00 34 12 00 00 00 00 00 00 04 00 00 00"
	response := "2026-10-26T02:47:41Z pldmd Rx: 08 01 00 3F 0B 00 04 00 00 00 DE AD BE EF"
	app := &App{selectFilterMode: "pldm_verbose", currentLogLines: []string{request, response}, filteredLogLines: []string{response}}
	pldm.BuildLineIndex(app.currentLogLines)
	app.updatePldmPanel(gui)
	got := view.Buffer()
	if !strings.Contains(got, "ReadFileByType") || !strings.Contains(got, "FileType (from request): 0x0003 (DUMP)") {
		t.Fatalf("parser panel omitted type:\n%s", got)
	}
}
