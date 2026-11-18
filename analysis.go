package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/awesome-gocui/gocui"
	"gopkg.in/yaml.v3"
)

//go:embed analysis.yml
var defaultAnalysisConfig []byte

type analysisPlugin struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Commands     []string `yaml:"commands"`
	Output       string   `yaml:"output"`
	RedfishPaths []string `yaml:"redfish_paths"`
	auth         *redfishAuth
}

func loadAnalysisPlugins(filename string) ([]analysisPlugin, error) {
	data := defaultAnalysisConfig
	if filename != "" {
		var err error
		data, err = os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
	}
	var config struct {
		Plugins []analysisPlugin `yaml:"plugins"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	if len(config.Plugins) == 0 {
		return nil, fmt.Errorf("no analysis plugins configured")
	}
	names := map[string]bool{}
	for _, plugin := range config.Plugins {
		name := strings.ToLower(strings.TrimSpace(plugin.Name))
		if name == "" || names[name] {
			return nil, fmt.Errorf("plugin names must be nonempty and unique: %q", plugin.Name)
		}
		names[name] = true
		if plugin.Output != "" && plugin.Output != "bios-values" && plugin.Output != "vmi-network" {
			return nil, fmt.Errorf("plugin %s has unknown output format %q", plugin.Name, plugin.Output)
		}
		if len(plugin.Commands) == 0 {
			return nil, fmt.Errorf("plugin %s has no commands", plugin.Name)
		}
		for _, command := range plugin.Commands {
			if strings.TrimSpace(command) == "" {
				return nil, fmt.Errorf("plugin %s has an empty command", plugin.Name)
			}
		}
	}
	for i := range config.Plugins {
		plugin := &config.Plugins[i]
		if strings.EqualFold(plugin.Name, "vmi") && plugin.Output == "bios-values" {
			plugin.Output = "vmi-network"
		}
		if plugin.Output == "vmi-network" && len(plugin.RedfishPaths) == 0 {
			plugin.RedfishPaths = []string{"/redfish/v1/Systems/hypervisor/EthernetInterfaces/eth0", "/redfish/v1/Systems/hypervisor/EthernetInterfaces/eth1"}
		}
	}
	return config.Plugins, nil
}

// Quote the whole command as one argument to the remote POSIX shell. OpenSSH
// joins remote arguments with spaces before passing them to the login shell.
func analysisSSHArgs(options []string, command string) []string {
	quoted := "'" + strings.ReplaceAll(command, "'", "'\"'\"'") + "'"
	return append(nonInteractiveSSHOptions(options), "sh", "-c", quoted)
}

type analysisUI struct {
	gui        *gocui.Gui
	plugins    []analysisPlugin
	options    []string
	selected   int
	cancel     context.CancelFunc
	done       chan struct{}
	running    bool
	workerDone <-chan struct{}
}

func (app *App) runAnalysisMode(configPath string, mock bool, auth redfishAuth) error {
	plugins, err := loadAnalysisPlugins(configPath)
	if err != nil {
		return err
	}
	for i := range plugins {
		if plugins[i].Output == "vmi-network" {
			if auth.password == nil {
				auth.reuseSSH(app.sshOptions, app.sshStatus, passwordBroker)
				if err := auth.prompt(); err != nil {
					return err
				}
				defer clear(auth.password)
			}
			plugins[i].auth = &auth
		}
	}
	output := gocui.OutputNormal
	if mock {
		output = gocui.OutputSimulator
	}
	gui, err := gocui.NewGui(output, true)
	if err != nil {
		return err
	}
	defer gui.Close()
	ui := &analysisUI{gui: gui, plugins: plugins, options: append([]string(nil), app.sshOptions...), done: make(chan struct{})}
	defer func() {
		close(ui.done)
		if ui.cancel != nil {
			ui.cancel()
		}
		if ui.workerDone != nil {
			<-ui.workerDone
		}
	}()
	gui.Mouse = app.mouseSupport
	gui.FgColor, gui.BgColor = app.foregroundColor, app.backgroundColor
	gui.SetManagerFunc(ui.layout)
	if err := ui.bindKeys(); err != nil {
		return err
	}
	if err := gui.MainLoop(); err != nil && !errors.Is(err, gocui.ErrQuit) {
		return err
	}
	return nil
}

func (ui *analysisUI) layout(gui *gocui.Gui) error {
	width, height := gui.Size()
	if width < 24 || height < 5 {
		return nil
	}
	left := min(20, max(10, width/6))
	list, err := gui.SetView("analysisPlugins", 0, 0, left-1, height-1, 0)
	if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
		return err
	}
	list.Title = " Modes (click/Enter) "
	list.Highlight = true
	list.SelFgColor = gocui.ColorBlack
	list.SelBgColor = gocui.ColorGreen
	list.Clear()
	for i, plugin := range ui.plugins {
		marker := "  "
		if i == ui.selected {
			marker = "> "
		}
		fmt.Fprintln(list, marker+plugin.Name)
	}
	_, h := list.Size()
	origin := max(0, ui.selected-h+1)
	list.SetOrigin(0, origin)
	list.SetCursor(0, ui.selected-origin)
	panel, err := gui.SetView("analysisOutput", left, 0, width-1, height-1, 0)
	if err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		panel.Wrap = true
		fmt.Fprint(panel, "Select a mode with Up/Down, then press Enter to run.\nTab: switch panel | Up/Down/PgUp/PgDn: scroll output\n1/2/3: Redfish / D-Bus / BIOS | Esc: cancel | Ctrl+C: quit\n")
		fmt.Fprintln(panel, ui.plugins[ui.selected].Description)
	}
	panel.Title = " Debug output "
	if ui.running {
		panel.Title = " Debug output — running (Esc cancels) "
	}
	if gui.CurrentView() == nil {
		_, err = gui.SetCurrentView("analysisPlugins")
		return err
	}
	return nil
}

func (ui *analysisUI) bindKeys() error {
	bind := func(view string, key interface{}, handler func(*gocui.Gui, *gocui.View) error) error {
		return ui.gui.SetKeybinding(view, key, gocui.ModNone, handler)
	}
	if err := bind("", gocui.KeyCtrlC, func(*gocui.Gui, *gocui.View) error { return gocui.ErrQuit }); err != nil {
		return err
	}
	if err := bind("", gocui.KeyEsc, func(*gocui.Gui, *gocui.View) error {
		if ui.cancel != nil {
			ui.cancel()
		}
		return nil
	}); err != nil {
		return err
	}
	if err := bind("", gocui.KeyTab, func(g *gocui.Gui, v *gocui.View) error {
		next := "analysisPlugins"
		if v != nil && v.Name() == next {
			next = "analysisOutput"
		}
		_, err := g.SetCurrentView(next)
		return err
	}); err != nil {
		return err
	}
	for _, entry := range []struct {
		key   gocui.Key
		delta int
	}{{gocui.KeyArrowUp, -1}, {gocui.KeyArrowDown, 1}} {
		delta := entry.delta
		if err := bind("analysisPlugins", entry.key, func(*gocui.Gui, *gocui.View) error {
			ui.selected = max(0, min(len(ui.plugins)-1, ui.selected+delta))
			return nil
		}); err != nil {
			return err
		}
	}
	if err := bind("", gocui.KeyEnter, ui.runSelected); err != nil {
		return err
	}

	if err := bind("", gocui.KeyCtrlJ, ui.runSelected); err != nil {
		return err
	}
	if err := bind("analysisPlugins", gocui.MouseLeft, ui.clickPlugin); err != nil {
		return err
	}
	if err := bind("analysisOutput", gocui.MouseLeft, func(g *gocui.Gui, _ *gocui.View) error {
		_, err := g.SetCurrentView("analysisOutput")
		return err
	}); err != nil {
		return err
	}
	for _, entry := range []struct {
		key   gocui.Key
		delta int
	}{{gocui.KeyArrowUp, -1}, {gocui.KeyArrowDown, 1}, {gocui.KeyPgup, -10}, {gocui.KeyPgdn, 10}} {
		delta := entry.delta
		if err := bind("analysisOutput", entry.key, func(_ *gocui.Gui, v *gocui.View) error {
			x, y := v.Origin()
			return v.SetOrigin(x, max(0, min(max(0, len(v.BufferLines())-1), y+delta)))
		}); err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		key   gocui.Key
		delta int
	}{{gocui.KeyArrowLeft, -8}, {gocui.KeyArrowRight, 8}} {
		delta := entry.delta
		if err := bind("analysisOutput", entry.key, func(_ *gocui.Gui, v *gocui.View) error {
			x, y := v.Origin()
			return v.SetOrigin(max(0, x+delta), y)
		}); err != nil {
			return err
		}
	}
	for _, stage := range []struct {
		key   rune
		title string
	}{{'1', "1. REDFISH"}, {'2', "2. D-BUS"}, {'3', "3. BIOS ATTRIBUTES"}} {
		title := stage.title
		if err := bind("", stage.key, func(g *gocui.Gui, _ *gocui.View) error {
			v, err := g.View("analysisOutput")
			if err != nil {
				return err
			}
			if strings.Contains(v.Buffer(), "VMI COMPARISON") {
				for _, line := range v.BufferLines() {
					if column := strings.Index(line, title); column >= 0 {
						v.SetOrigin(max(0, column-22), 0)
						_, err = g.SetCurrentView("analysisOutput")
						return err
					}
				}
			}
			for row, line := range v.ViewBufferLines() {
				if strings.HasPrefix(line, title) {
					v.SetOrigin(0, row)
					_, err = g.SetCurrentView("analysisOutput")
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// All view writes occur on the GUI loop; command execution never blocks it.
type analysisWriter struct {
	gui  *gocui.Gui
	done <-chan struct{}
}

func (w analysisWriter) Write(p []byte) (int, error) {
	text := string(p)
	applied := make(chan struct{})
	w.gui.UpdateAsync(func(g *gocui.Gui) error {
		defer close(applied)
		v, err := g.View("analysisOutput")
		if err == nil {
			fmt.Fprint(v, text)
		}
		return nil
	})
	select {
	case <-applied:
		return len(p), nil
	case <-w.done:
		return 0, context.Canceled
	}
}

func (ui *analysisUI) runSelected(g *gocui.Gui, _ *gocui.View) error {
	if ui.running {
		return nil
	}
	panel, err := g.View("analysisOutput")
	if err != nil {
		return err
	}
	plugin := ui.plugins[ui.selected]
	panel.Clear()
	panel.SetOrigin(0, 0)
	count := len(plugin.Commands)
	if plugin.Output == "vmi-network" {
		count += len(plugin.RedfishPaths) + 22
	}
	fmt.Fprintf(panel, "%s\n%s\nRunning %d commands over SSH…\n\n", plugin.Name, plugin.Description, count)
	ctx, cancel := context.WithCancel(context.Background())
	ui.cancel, ui.running = cancel, true
	finished := make(chan struct{})
	ui.workerDone = finished
	go func() {
		defer close(finished)
		defer cancel()
		writer := analysisWriter{gui: g, done: ui.done}

		err := runAnalysisCommands(ctx, ui.options, plugin, writer)
		if ctx.Err() != nil {
			fmt.Fprintln(writer, "\nAnalysis cancelled.")
		} else if err != nil && (plugin.Output == "bios-values" || plugin.Output == "vmi-network") {
			fmt.Fprintln(writer, "\nCompleted with errors. See failed attributes above.")
		} else if err != nil {
			fmt.Fprintf(writer, "\nAnalysis stopped: %v\n", err)
		} else {
			fmt.Fprintln(writer, "\nAnalysis finished.")
		}

		g.UpdateAsync(func(*gocui.Gui) error { ui.running = false; ui.cancel = nil; return nil })
	}()
	return nil
}

func runAnalysisCommands(ctx context.Context, options []string, plugin analysisPlugin, writer io.Writer) error {
	if plugin.Output == "vmi-network" {
		return runVMINetwork(ctx, options, plugin, writer)
	}
	if plugin.Output == "bios-values" {
		return runBIOSValueCommands(ctx, options, plugin.Commands, writer)
	}
	for _, command := range plugin.Commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(writer, "$ %s\n", command)
		commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.CommandContext(commandCtx, "ssh", analysisSSHArgs(options, command)...)
		cmd.WaitDelay = 2 * time.Second
		cmd.Stdout, cmd.Stderr = writer, writer
		err := cmd.Run()
		ctxErr := commandCtx.Err()
		cancel()
		if ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return fmt.Errorf("command %q: %w", command, err)
		}
		fmt.Fprintln(writer)
	}
	return nil
}

// BIOS queries return JSON values whose type varies by attribute (string or number).
// Keep querying after an individual failure so all interface attributes are shown.
func runBIOSValueCommands(ctx context.Context, options []string, commands []string, writer io.Writer) error {
	var failures []error
	results := make([]biosDisplayValue, len(commands))
	for i, command := range commands {
		results[i].attribute = biosCommandAttribute(command)
		results[i].command = command
	}
	publish := func(active int) {
		if report, ok := writer.(interface{ WriteBIOSValues([]biosDisplayValue, int) }); ok {
			report.WriteBIOSValues(results, active)
			return
		}
		if report, ok := writer.(interface{ WriteReport(string) error }); ok {
			report.WriteReport(formatBIOSReport(results, active))
		}
	}
	for index, command := range commands {
		if err := ctx.Err(); err != nil {
			return err
		}
		label := results[index].attribute
		publish(index)
		commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.CommandContext(commandCtx, "ssh", analysisSSHArgs(options, command)...)
		cmd.WaitDelay = 2 * time.Second
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		ctxErr := commandCtx.Err()
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if ctxErr != nil {
			err = ctxErr
		}
		if err == nil {
			var response map[string]json.RawMessage
			err = json.Unmarshal(stdout.Bytes(), &response)
			if err == nil {
				value, exists := response["CurrentValue"]
				if !exists {
					err = fmt.Errorf("response has no CurrentValue")
				} else {
					var text string
					if json.Unmarshal(value, &text) == nil {
						results[index].value = terminalSafeText(text, 4096)
					} else {
						results[index].value = string(value)
					}
				}
			}
		}
		if err != nil {
			detail := terminalSafeText(stderr.String(), 800)
			if detail == "" {
				detail = terminalSafeText(stdout.String(), 800)
			}
			results[index].err = strings.TrimSpace(fmt.Sprintf("%v %s", err, detail))
			failures = append(failures, fmt.Errorf("%s: %w", label, err))
		}
		results[index].done = true
		publish(-1)
	}
	if _, ok := writer.(interface{ WriteReport(string) error }); !ok {
		fmt.Fprint(writer, formatBIOSReport(results, -1))
	}
	return errors.Join(failures...)
}

func (ui *analysisUI) clickPlugin(g *gocui.Gui, v *gocui.View) error {
	_, y := g.MousePosition()
	_, top, _, _ := v.Dimensions()
	_, origin := v.Origin()
	index := y - top - 1 + origin
	if index < 0 || index >= len(ui.plugins) {
		return nil
	}
	ui.selected = index
	if _, err := g.SetCurrentView("analysisPlugins"); err != nil {
		return err
	}
	return ui.runSelected(g, v)
}

type biosDisplayValue struct {
	attribute, value, err string
	command               string
	done                  bool
}

func biosCommandAttribute(command string) string {
	fields := strings.Fields(command)
	for i, field := range fields {
		if field == "-a" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return command
}

// ANSI colour helpers used in VMI analysis output.
const (
	ansiReset       = "\033[0m"
	ansiBold        = "\033[1m"
	ansiDim         = "\033[2m"
	ansiCyan        = "\033[36m"
	ansiBoldCyan    = "\033[1;36m"
	ansiYellow      = "\033[33m"
	ansiBoldYellow  = "\033[1;33m"
	ansiGreen       = "\033[32m"
	ansiRed         = "\033[31m"
	ansiBoldRed     = "\033[1;31m"
	ansiBoldMagenta = "\033[1;35m"
	ansiBoldBlue    = "\033[1;34m"
	ansiGray        = "\033[90m"
)

func formatBIOSReport(results []biosDisplayValue, active int) string {
	var out strings.Builder
	completed, failed := 0, 0
	for _, result := range results {
		if result.done {
			completed++
		}
		if result.err != "" {
			failed++
		}
	}
	// ── Header banner ────────────────────────────────────────────────────────
	banner := "  VMI NETWORK CONFIGURATION  "
	fmt.Fprintf(&out, "\n%s%s%s%s\n", ansiBoldCyan, ansiBold, banner, ansiReset)
	fmt.Fprintf(&out, "%sCurrent BIOS values from the BMC%s\n", ansiCyan, ansiReset)
	progressColor := ansiGreen
	if failed > 0 {
		progressColor = ansiRed
	}
	fmt.Fprintf(&out, "%sProgress: %d/%d queried%s", progressColor, completed, len(results), ansiReset)
	if failed > 0 {
		fmt.Fprintf(&out, "  %s✖ %d failed%s", ansiBoldRed, failed, ansiReset)
	}
	fmt.Fprintln(&out)
	previousGroup := ""
	for index, result := range results {
		group, field := "Other attributes", result.attribute
		parts := strings.SplitN(result.attribute, "_", 4)
		if len(parts) == 4 && parts[0] == "vmi" && strings.HasPrefix(parts[1], "if") {
			if number, err := strconv.Atoi(strings.TrimPrefix(parts[1], "if")); err == nil {
				group = fmt.Sprintf("Interface %d / %s", number, strings.ToUpper(parts[2]))
				field = parts[3]
			}
		}
		labels := map[string]string{"ipaddr": "IP address", "gateway": "Gateway", "method": "Method", "prefix_length": "Prefix length", "enabled": "Enabled", "interface_enabled": "IF enabled", "dhcp_enabled": "DHCP enabled", "dhcp_mode": "DHCPv6 mode", "autoconfig": "Autoconfig"}
		if label, ok := labels[field]; ok {
			field = label
		}
		if group != previousGroup {
			divider := strings.Repeat("─", len(group)+4)
			fmt.Fprintf(&out, "\n%s┌%s┐%s\n", ansiBoldMagenta, divider, ansiReset)
			fmt.Fprintf(&out, "%s│  %s%s%s%s  │%s\n", ansiBoldMagenta, ansiBold, group, ansiReset, ansiBoldMagenta, ansiReset)
			fmt.Fprintf(&out, "%s└%s┘%s\n", ansiBoldMagenta, divider, ansiReset)
			previousGroup = group
		}
		value := "Pending"
		if index == active {
			value = "Querying…"
		}
		if result.done {
			value = result.value
			if value == "" {
				value = "(empty)"
			}
		}
		if result.err != "" {
			value = "ERROR"
		}
		fmt.Fprintf(&out, "  %-14s : %s\n", field, value)
		if result.command != "" {
			fmt.Fprintf(&out, "    %s$ %s%s\n", ansiGray, result.command, ansiReset)
		}
		if result.err != "" {
			fmt.Fprintf(&out, "    %s%s%s\n", ansiGray, result.attribute, ansiReset)
			fmt.Fprintf(&out, "    %s%s%s\n", ansiRed, result.err, ansiReset)
		}
	}
	return out.String()
}

// Replace the report as queries finish, keeping one stable, readable dashboard.
func (w analysisWriter) WriteReport(text string) error {
	applied := make(chan struct{})
	w.gui.UpdateAsync(func(g *gocui.Gui) error {
		defer close(applied)
		if v, err := g.View("analysisOutput"); err == nil {
			x, y := v.Origin()
			v.Clear()
			v.Wrap = !strings.HasPrefix(text, "VMI COMPARISON")
			fmt.Fprint(v, text)
			v.SetOrigin(x, y)
		}
		return nil
	})
	select {
	case <-applied:
		return nil
	case <-w.done:
		return context.Canceled
	}
}
