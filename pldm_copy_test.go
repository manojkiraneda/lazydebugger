package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/awesome-gocui/gocui"
)

func TestPldmSelectedText(t *testing.T) {
	lines := []string{"Header", "  SensorID: 42", "  State: 正常", "Payload"}
	for _, tc := range []struct {
		selection pldmTextSelection
		all       bool
		want      string
	}{
		{pldmTextSelection{active: true, anchor: 1, end: 2}, false, "  SensorID: 42\n  State: 正常"},
		{pldmTextSelection{active: true, anchor: 2, end: 1}, false, "  SensorID: 42\n  State: 正常"},
		{pldmTextSelection{active: true, anchor: 1, end: 1}, false, "  SensorID: 42"},
		{pldmTextSelection{}, false, strings.Join(lines, "\n")},
		{pldmTextSelection{active: true, anchor: 1, end: 2}, true, strings.Join(lines, "\n")},
	} {
		if got := tc.selection.text(lines, tc.all); got != tc.want {
			t.Fatalf("copied %q, want %q", got, tc.want)
		}
	}
}

func TestPldmSelectionScrollingAndRefresh(t *testing.T) {
	gui, err := gocui.NewGui(gocui.OutputSimulator, true)
	if err != nil {
		t.Fatal(err)
	}
	defer gui.Close()
	view, err := gui.SetView("docker", 0, 0, 60, 5, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		t.Fatal(err)
	}
	fmt.Fprint(view, "Header\n  SensorID: 42\n  State: 正常\nPayload\nAnother line\nLast line")
	app := &App{selectFilterMode: "pldm_verbose", filteredLogLines: []string{"pldmd Rx: 85 02 0a"}}
	app.pldmTextSelection = pldmTextSelection{active: true, anchor: 1, end: 1}
	app.scrollPldmPanelDown(gui, 2)
	if got := app.pldmTextSelection.text(view.BufferLines(), false); got != "  SensorID: 42\n  State: 正常\nPayload" {
		t.Fatalf("scroll did not extend selection: %q", got)
	}
	before := view.Buffer()
	app.updatePldmPanel(gui)
	if got := view.Buffer(); got != before {
		t.Fatal("refresh overwrote selected text")
	}
	app.scrollPldmPanelUp(gui, 100)
	if app.pldmTextSelection.end != 0 {
		t.Fatal("selection did not clamp at start")
	}
	app.scrollPldmPanelDown(gui, 100)
	if app.pldmTextSelection.end != view.LinesHeight()-1 {
		t.Fatal("selection did not clamp at end")
	}
	app.clearPldmTextSelection(view)
	if app.pldmTextSelection.active {
		t.Fatal("selection did not clear")
	}
	if view.Buffer() != before {
		t.Fatal("highlight changed copied text")
	}
	app.updatePldmPanel(gui)
	if view.Buffer() == before {
		t.Fatal("clearing selection did not allow refresh")
	}
	app.pldmTextSelection = pldmTextSelection{active: true, anchor: 0, end: 1}
	app.leavePldmParser(gui)
	if app.pldmTextSelection.active || view.Subtitle != "" || !view.Highlight || !view.Autoscroll {
		t.Fatal("leaving PLDM mode did not restore the container panel")
	}
}

func TestClipboardOSC52(t *testing.T) {
	text := "SensorID: 42\nState: 正常\x1b[31m"
	for _, tmux := range []bool{false, true} {
		var output bytes.Buffer
		if err := writeClipboardOSC52(&output, text, tmux); err != nil {
			t.Fatal(err)
		}
		want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
		if tmux {
			want = "\x1bPtmux;" + strings.ReplaceAll(want, "\x1b", "\x1b\x1b") + "\x1b\\"
		}
		if output.String() != want {
			t.Fatalf("unexpected clipboard sequence: %q", output.String())
		}
	}
	var output bytes.Buffer
	if err := writeClipboardOSC52(&output, strings.Repeat("a", 75001), false); err == nil || output.Len() != 0 {
		t.Fatal("oversized copy must fail without writing a truncated sequence")
	}
}

func TestCopyParserTextUsesLocalClipboard(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux clipboard helper test")
	}
	dir := t.TempDir()
	destination := filepath.Join(dir, "clipboard")
	if err := os.WriteFile(filepath.Join(dir, "xclip"), []byte("#!/bin/sh\n/bin/cat > \"$PLDM_COPY_TEST_OUTPUT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":test")
	t.Setenv("PLDM_COPY_TEST_OUTPUT", destination)
	want := "  SensorID: 42\n  State: 正常"
	if err := copyParserText(want); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != want {
		t.Fatalf("clipboard = %q, %v", got, err)
	}
}
