package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBackgroundSSHOptionsDoNotPromptOrShareArgumentStorage(t *testing.T) {
	options := nonInteractiveSSHOptions([]string{"host", "-o", "BatchMode=no"})
	if !reflect.DeepEqual(options[:7], []string{"-T", "-o", "BatchMode=yes", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no"}) {
		t.Fatalf("unsafe background options: %q", options)
	}
	first := append(options, "pldmtool", "platform", "getpdr")
	_ = append(options, "docker", "ps", "-a")
	if first[len(options)] != "pldmtool" {
		t.Fatal("concurrent command overwrote PDR arguments")
	}
}

func TestFetchPLDMPDRs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake SSH executable uses a POSIX shell")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SSH_PDR_ARGS"
case "$SSH_PDR_SCENARIO" in
success) printf '[{"PDRType":"State Sensor PDR","sensorID":42}]'; printf 'retry diagnostic\n' >&2 ;;
auth) printf 'Permission denied (publickey,password).\n' >&2; exit 255 ;;
missing) printf 'sh: pldmtool: not found\n' >&2; exit 127 ;;
truncated) printf '[{"PDRType":"State Sensor PDR"}';;
opening-bracket) printf '[';;
incomplete-with-flag)
 case "$*" in
  *'getpdr -a -n 2'*) printf '[' ;;
  *) printf '[{"PDRType":"State Sensor PDR","sensorID":42}]' ;;
 esac ;;
empty) printf '[]';;
null) printf 'null';;
wrong-record) printf '[{"error":"transport failed"}]';;
failed-with-json) printf '[{"PDRType":"State Sensor PDR"}]'; printf 'transport failed\n' >&2; exit 1 ;;
unsupported|unsupported-generic|unsupported-exact|unsupported-exit-zero)
 case "$*" in
  *'getpdr -a -n 2'*)
   if [ "$SSH_PDR_SCENARIO" = unsupported-exact ]; then
    printf 'The following arguments were not expected: 2 -n\nRun with --help for more information.\n' >&2
   elif [ "$SSH_PDR_SCENARIO" = unsupported-exit-zero ]; then
    printf 'The following arguments were not expected: 2 -n\nRun with --help for more information.\n'
    exit 0
   elif [ "$SSH_PDR_SCENARIO" = unsupported-generic ]; then
    printf 'Invalid arguments\n'
   else
    printf 'The following arguments were not expected: -n 2\n' >&2
   fi
   exit 109 ;;
  *) printf '[{"PDRType":"State Sensor PDR"}]' ;;
 esac ;;
timeout) exec sleep 5 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tt := range []struct {
		scenario string
		wantErr  string
	}{
		{"success", ""}, {"auth", "Permission denied"}, {"missing", "not found"},
		{"truncated", "invalid JSON"}, {"empty", "empty PDR repository"},
		{"opening-bracket", "invalid JSON"}, {"incomplete-with-flag", ""},
		{"null", "empty PDR repository"}, {"wrong-record", "no PDRType"},
		{"failed-with-json", "transport failed"}, {"unsupported", ""}, {"unsupported-generic", ""},
		{"unsupported-exact", ""}, {"unsupported-exit-zero", ""},
		{"timeout", "context deadline exceeded"},
	} {
		t.Run(tt.scenario, func(t *testing.T) {
			t.Setenv("SSH_PDR_SCENARIO", tt.scenario)
			argsFile := filepath.Join(t.TempDir(), "args")
			t.Setenv("SSH_PDR_ARGS", argsFile)
			ctx := context.Background()
			if tt.scenario == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			output, count, err := fetchPLDMPDRs(ctx, []string{"service@bmc"})
			args, readErr := os.ReadFile(argsFile)
			if readErr != nil {
				t.Fatal(readErr)
			}
			lines := strings.Split(strings.TrimSpace(string(args)), "\n")
			wantAttempts := 1
			switch tt.scenario {
			case "auth", "missing", "truncated", "opening-bracket", "incomplete-with-flag", "failed-with-json", "unsupported", "unsupported-generic", "unsupported-exact", "unsupported-exit-zero", "empty", "null", "wrong-record":
				wantAttempts = 2
			}
			if len(lines) != wantAttempts || !strings.HasSuffix(lines[0], "sudo -n pldmtool platform getpdr -a -n 2") ||
				(wantAttempts == 2 && !strings.HasSuffix(lines[1], "sudo -n pldmtool platform getpdr -a")) {
				t.Fatalf("retry args = %q", lines)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if count != 0 || output != nil {
					t.Fatal("failed fetch exposed partial records")
				}
				if tt.scenario == "opening-bracket" && !strings.Contains(err.Error(), "command: sudo -n pldmtool platform getpdr -a") {
					t.Fatalf("missing fallback command in diagnostic: %v", err)
				}
				if wantAttempts == 2 && (!strings.Contains(err.Error(), "fallback without -n 2 failed; command: sudo -n pldmtool platform getpdr -a:") ||
					strings.Contains(err.Error(), "getpdr -a -n 2:")) {
					t.Fatalf("incorrect fallback diagnostic: %v", err)
				}
				return
			}
			if err != nil || count != 1 || len(output) == 0 {
				t.Fatalf("fetch = %s, %d, %v", output, count, err)
			}
		})
	}
}

func TestPDRDiagnosticCannotInjectTerminalControls(t *testing.T) {
	got := terminalSafeText("bad\x1b[6n\r\n\x00message", 100)
	if strings.ContainsAny(got, "\x1b\r\n\x00") {
		t.Fatalf("unsafe diagnostic: %q", got)
	}
	if got := terminalSafeText("abcdef", 3); got != "abc…" {
		t.Fatalf("unbounded diagnostic: %q", got)
	}
}
