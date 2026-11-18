package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/awesome-gocui/gocui"
)

func TestAnalysisConfig(t *testing.T) {
	plugins, err := loadAnalysisPlugins("")
	if err != nil || len(plugins) != 1 || plugins[0].Name != "vmi" {
		t.Fatalf("default plugins: %v, %v", plugins, err)
	}
	for _, config := range []string{
		"plugins: []", "plugins: [{name: vmi}]", "plugins: [{name: vmi, commands: ['']}]",
		"plugins: [{name: vmi, commands: [uptime]}, {name: VMI, commands: [hostname]}]",
		"plugins: [{name: vmi, command: uptime}]",
	} {
		file := filepath.Join(t.TempDir(), "plugins.yml")
		if err := os.WriteFile(file, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadAnalysisPlugins(file); err == nil {
			t.Errorf("accepted invalid config %s", config)
		}
	}
}

func TestAnalysisTwoPanelLayout(t *testing.T) {
	gui, err := gocui.NewGui(gocui.OutputSimulator, true)
	if err != nil {
		t.Fatal(err)
	}
	defer gui.Close()
	plugins, err := loadAnalysisPlugins("")
	if err != nil {
		t.Fatal(err)
	}
	ui := &analysisUI{gui: gui, plugins: plugins}
	if err := ui.layout(gui); err != nil {
		t.Fatal(err)
	}
	if len(gui.Views()) != 2 {
		t.Fatalf("got %d views, want two", len(gui.Views()))
	}
	list, err := gui.View("analysisPlugins")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.Buffer(), "> vmi") {
		t.Fatal("VMI absent from mode list")
	}
	if err := ui.bindKeys(); err != nil {
		t.Fatal(err)
	}
}

func TestAnalysisRemoteShellQuoting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell test")
	}
	command := `printf '%s' "a'b $HOME" | sed 's/$/ done/'`
	args := analysisSSHArgs([]string{"bmc"}, command)
	// Reproduce OpenSSH's remote argument joining and login shell parsing.
	cmd := exec.Command("sh", "-c", strings.Join(args[len(args)-3:], " "))
	cmd.Env = append(os.Environ(), "HOME=/test-home")
	got, err := cmd.CombinedOutput()
	if err != nil || string(got) != "a'b /test-home done" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestAnalysisCommandsStopOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake SSH")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in\n*fail*) echo diagnostic >&2; exit 7;;\n*) echo output;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var output bytes.Buffer
	plugin := analysisPlugin{Name: "vmi", Commands: []string{"hostname", "fail", "never-run"}}
	err := runAnalysisCommands(context.Background(), []string{"bmc"}, plugin, &output)
	if err == nil || !strings.Contains(output.String(), "output") || !strings.Contains(output.String(), "diagnostic") || strings.Contains(output.String(), "never-run") {
		t.Fatalf("output %q, error %v", output.String(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output.Reset()
	if err := runAnalysisCommands(ctx, []string{"bmc"}, plugin, &output); err != context.Canceled || output.Len() != 0 {
		t.Fatalf("cancelled run: %q %v", output.String(), err)
	}
}

func TestVMIQueriesAndPrintsAllAttributes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake SSH")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
 *vmi_if0_ipv4_gateway*) echo unavailable >&2; exit 1;;
 *vmi_if0_ipv6_gateway*) echo '{bad json';;
 *vmi_if1_ipv4_gateway*) echo '{}';;
 *prefix_length*) echo '{"CurrentValue":0}';;
 *method*) echo '{"CurrentValue":"Static"}';;
 *) echo '{"CurrentValue":"0.0.0.0"}';;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	plugins, err := loadAnalysisPlugins("")
	if err != nil {
		t.Fatal(err)
	}
	plugins[0].Output = "bios-values"
	if len(plugins[0].Commands) != 16 {
		t.Fatalf("expected 16 queries, got %d", len(plugins[0].Commands))
	}
	var output bytes.Buffer
	err = runAnalysisCommands(context.Background(), []string{"bmc"}, plugins[0], &output)
	if err == nil {
		t.Fatal("expected aggregate query error")
	}
	for iface := 0; iface < 2; iface++ {
		for _, version := range []int{4, 6} {
			for _, field := range []string{"ipaddr", "gateway", "method", "prefix_length"} {
				attribute := fmt.Sprintf("vmi_if%d_ipv%d_%s", iface, version, field)
				value := "0.0.0.0"
				if field == "prefix_length" {
					value = "0"
				}
				if field == "method" {
					value = "Static"
				}
				if field == "gateway" && (iface == 0 || version == 4) {
					value = "ERROR:"
				}
				group := fmt.Sprintf("Interface %d / IPV%d", iface, version)
				section := strings.SplitN(output.String(), group, 2)
				if len(section) < 2 {
					t.Fatalf("missing %s", group)
				}
				body := strings.SplitN(section[1], "Interface ", 2)[0]
				label := map[string]string{"ipaddr": "IP address", "gateway": "Gateway", "method": "Method", "prefix_length": "Prefix length"}[field]
				if value == "ERROR:" {
					value = "ERROR"
				}
				if !strings.Contains(body, fmt.Sprintf("  %-14s : %s", label, value)) {
					t.Errorf("missing %s: %s in\n%s", attribute, value, output.String())
				}
			}
		}
	}
}

func TestAnalysisEnterRunsFromEitherPanel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake SSH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\necho '{\"CurrentValue\":\"192.0.2.5\"}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	gui, err := gocui.NewGui(gocui.OutputSimulator, true)
	if err != nil {
		t.Fatal(err)
	}
	defer gui.Close()
	ui := &analysisUI{gui: gui, options: []string{"bmc"}, done: make(chan struct{}), plugins: []analysisPlugin{{Name: "vmi", Output: "bios-values", Commands: []string{"pldmtool bios GetBIOSAttributeCurrentValueByHandle -a vmi_if0_ipv4_ipaddr"}}}}
	gui.SetManagerFunc(ui.layout)
	if err := ui.bindKeys(); err != nil {
		t.Fatal(err)
	}
	screen := gui.GetTestingScreen()
	stop := screen.StartGui()
	defer stop()
	defer close(ui.done)
	for _, key := range []gocui.Key{gocui.KeyEnter, gocui.KeyCtrlJ} {
		// Run once in the mode list, then again with output focused.
		screen.SendKeySync(key)
		deadline := time.After(3 * time.Second)
		for {
			result := make(chan bool, 1)
			gui.UpdateAsync(func(g *gocui.Gui) error {
				v, _ := g.View("analysisOutput")
				result <- !ui.running && strings.Contains(v.Buffer(), "IP address     : 192.0.2.5")
				return nil
			})
			if <-result {
				break
			}
			select {
			case <-deadline:
				t.Fatal("Enter did not run plugin or render SSH results")
			case <-time.After(10 * time.Millisecond):
			}
		}
		screen.SendKeySync(gocui.KeyTab)
	}
}
