package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/awesome-gocui/gocui"
)

type pldmTextSelection struct {
	active, dragging bool
	anchor, end      int
}

func (s pldmTextSelection) bounds() (int, int) {
	return min(s.anchor, s.end), max(s.anchor, s.end)
}

func (s pldmTextSelection) text(lines []string, all bool) string {
	if len(lines) == 0 {
		return ""
	}
	if !s.active || all {
		return strings.Join(lines, "\n")
	}
	start, end := s.bounds()
	start = max(0, start)
	end = min(len(lines)-1, end)
	if start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}

func (app *App) clearPldmTextSelection(v *gocui.View) {
	if app.pldmTextSelection.active {
		start, end := app.pldmTextSelection.bounds()
		for line := start; line <= min(end, v.LinesHeight()-1); line++ {
			_ = v.SetHighlight(line, false)
		}
	}
	app.pldmTextSelection = pldmTextSelection{}
}

func (app *App) leavePldmParser(g *gocui.Gui) {
	if v, err := g.View("docker"); err == nil {
		app.clearPldmTextSelection(v)
		v.Subtitle = ""
		v.Highlight = true
		v.Autoscroll = true
	} else {
		app.pldmTextSelection = pldmTextSelection{}
	}
}

func (app *App) extendPldmTextSelection(v *gocui.View, line int) {
	if !app.pldmTextSelection.active || v.LinesHeight() == 0 {
		return
	}
	selection := app.pldmTextSelection
	app.clearPldmTextSelection(v)
	selection.end = max(0, min(line, v.LinesHeight()-1))
	app.pldmTextSelection = selection
	v.Highlight = false
	v.SelFgColor = app.selectedForegroundColor
	v.SelBgColor = app.selectedBackgroundColor
	start, end := selection.bounds()
	for row := start; row <= end; row++ {
		_ = v.SetHighlight(row, true)
	}
}

func (app *App) movePldmTextSelection(v *gocui.View, delta int) {
	line := max(0, min(app.pldmTextSelection.end+delta, v.LinesHeight()-1))
	app.extendPldmTextSelection(v, line)
	_, origin := v.Origin()
	_, height := v.Size()
	if line < origin {
		_ = v.SetOrigin(0, line)
	} else if line >= origin+height {
		_ = v.SetOrigin(0, max(0, line-height+1))
	}
}

func (app *App) selectPldmTextMouse(g *gocui.Gui, v *gocui.View) error {
	if err := app.setSelectView(g, "docker"); err != nil {
		return err
	}
	_, mouseY := g.MousePosition()
	_, top, _, _ := v.Dimensions()
	_, origin := v.Origin()
	line := max(0, min(origin+mouseY-top-1, v.LinesHeight()-1))
	if !app.pldmTextSelection.dragging {
		app.clearPldmTextSelection(v)
		app.pldmTextSelection = pldmTextSelection{active: true, dragging: true, anchor: line, end: line}
	}
	app.extendPldmTextSelection(v, line)
	return nil
}

func (app *App) setupPldmTextKeybindings() error {
	type binding struct {
		key     interface{}
		handler func(*gocui.Gui, *gocui.View) error
	}
	bindings := []binding{
		{'v', func(g *gocui.Gui, v *gocui.View) error {
			if app.pldmTextSelection.active {
				app.clearPldmTextSelection(v)
				app.updatePldmPanel(g)
			} else {
				_, line := v.Origin()
				app.pldmTextSelection = pldmTextSelection{active: true, anchor: line, end: line}
				app.extendPldmTextSelection(v, line)
			}
			return nil
		}},
		{gocui.KeyEsc, func(g *gocui.Gui, v *gocui.View) error {
			app.clearPldmTextSelection(v)
			app.updatePldmPanel(g)
			return nil
		}},
		{gocui.MouseRelease, func(g *gocui.Gui, v *gocui.View) error {
			app.pldmTextSelection.dragging = false
			return nil
		}},
	}
	for _, all := range []bool{false, true} {
		key := 'y'
		if all {
			key = 'Y'
		}
		bindings = append(bindings, binding{key, func(g *gocui.Gui, v *gocui.View) error {
			text := app.pldmTextSelection.text(v.BufferLines(), all)
			if text == "" {
				v.Subtitle = " No parser text to copy "
				return nil
			}
			if err := copyParserText(text); err != nil {
				v.Subtitle = " Copy failed: " + err.Error() + " "
			} else {
				v.Subtitle = " Copy sent | v: select | y: copy | Y: all | Esc: clear "
			}
			return nil
		}})
	}
	for _, binding := range bindings {
		handler := binding.handler
		if err := app.gui.SetKeybinding("docker", binding.key, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
			if app.selectFilterMode != "pldm_verbose" {
				return nil
			}
			return handler(g, v)
		}); err != nil {
			return err
		}
	}
	return nil
}

// Prefer the local clipboard, then ask the terminal via OSC 52. The latter
// also works when the app runs over SSH, subject to terminal permissions.
func copyParserText(text string) error {
	var commands [][]string
	switch runtime.GOOS {
	case "darwin":
		commands = [][]string{{"pbcopy"}}
	case "windows":
		commands = [][]string{{"clip.exe"}}
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			commands = append(commands, []string{"wl-copy"})
		}
		if os.Getenv("DISPLAY") != "" {
			commands = append(commands, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
		}
	}
	for _, args := range commands {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		err := cmd.Run()
		cancel()
		if err == nil {
			return nil
		}
	}
	return writeClipboardOSC52(os.Stdout, text, os.Getenv("TMUX") != "")
}

func writeClipboardOSC52(w io.Writer, text string, tmux bool) error {
	// Avoid silently truncating text on terminals with small OSC limits.
	if len(text) > 75000 {
		return fmt.Errorf("text exceeds terminal clipboard limit; install a clipboard helper")
	}
	sequence := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if tmux {
		sequence = "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	_, err := io.WriteString(w, sequence)
	return err
}
