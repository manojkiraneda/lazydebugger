package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUnixInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash installer")
	}
	script, err := filepath.Abs("scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fresh Linux", "fresh macOS", "upgrade", "config symlink", "older release", "failed download"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			helpers := filepath.Join(dir, "helpers")
			binDir := filepath.Join(dir, "bin with spaces")
			configDir := filepath.Join(dir, "config with spaces")
			configPath := filepath.Join(configDir, "config.yml")
			logPath := filepath.Join(dir, "requests")
			for _, path := range []string{helpers, configDir} {
				if err := os.MkdirAll(path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			curl := `#!/bin/sh
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) output=$2; shift 2 ;;
        -w) shift 2 ;;
        https:*) url=$1; shift ;;
        *) shift ;;
    esac
done
printf '%s\n' "$url" >> "$TEST_INSTALL_LOG"
case "$url" in
    https://github.com/manojkiraneda/lazydebugger/releases/latest)
        printf 'https://github.com/manojkiraneda/lazydebugger/releases/tag/v9.8.7'; exit 0 ;;
    https://github.com/manojkiraneda/lazydebugger/releases/download/v9.8.7/config.yml)
        [ "$TEST_INSTALL_CASE" != 'older release' ] || exit 22
        printf 'sample config\n' > "$output" ;;
    https://raw.githubusercontent.com/manojkiraneda/lazydebugger/v9.8.7/config.yml)
        printf 'sample config\n' > "$output" ;;
    https://github.com/manojkiraneda/lazydebugger/releases/download/v9.8.7/lazydebugger-v9.8.7-*)
        [ "$TEST_INSTALL_CASE" != 'failed download' ] || exit 22
        printf '#!/bin/sh\necho v9.8.7\n' > "$output" ;;
    *) exit 99 ;;
esac
`
			uname := "#!/bin/sh\ncase \"$1\" in\n-s) echo Linux ;;\n-m) echo x86_64 ;;\nesac\n"
			if name == "fresh macOS" {
				uname = strings.ReplaceAll(strings.ReplaceAll(uname, "Linux", "Darwin"), "x86_64", "arm64")
			}
			for filename, content := range map[string]string{"curl": curl, "uname": uname, "xattr": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(helpers, filename), []byte(content), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if name == "upgrade" {
				if err := os.WriteFile(configPath, []byte("my settings\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "config symlink" {
				target := filepath.Join(dir, "my config.yml")
				if err := os.WriteFile(target, []byte("my settings\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, configPath); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(), "PATH="+helpers+string(os.PathListSeparator)+os.Getenv("PATH"),
				"LAZYDEBUGGER_BIN_DIR="+binDir, "LAZYDEBUGGER_CONFIG_DIR="+configDir, "LAZYDEBUGGER_VERSION=",
				"TEST_INSTALL_LOG="+logPath, "TEST_INSTALL_CASE="+name)
			output, err := cmd.CombinedOutput()
			if name == "failed download" {
				if err == nil {
					t.Fatal("failed download reported success")
				}
				if _, err := os.Stat(filepath.Join(binDir, "lazydebugger")); !os.IsNotExist(err) {
					t.Fatal("installed a failed download")
				}
				return
			}
			if err != nil {
				t.Fatalf("install: %v\n%s", err, output)
			}
			got, err := os.ReadFile(configPath)
			want := "sample config\n"
			if name == "upgrade" || name == "config symlink" {
				want = "my settings\n"
			}
			if err != nil || string(got) != want {
				t.Fatalf("config = %q, %v", got, err)
			}
			info, err := os.Stat(filepath.Join(binDir, "lazydebugger"))
			if err != nil || info.Mode()&0111 == 0 {
				t.Fatal("binary not installed as executable")
			}
			requests, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(requests), "lazyjournal") {
				t.Fatal("downloaded upstream app")
			}
			if (name == "upgrade" || name == "config symlink") && strings.Contains(string(requests), "/config.yml") {
				t.Fatal("upgrade downloaded a replacement config")
			}
			if name == "fresh macOS" && !strings.Contains(string(requests), "darwin-arm64") {
				t.Fatal("wrong macOS binary")
			}
			if name == "older release" && !strings.Contains(string(requests), "/v9.8.7/config.yml") {
				t.Fatal("fallback config is not pinned to release")
			}
		})
	}
}
