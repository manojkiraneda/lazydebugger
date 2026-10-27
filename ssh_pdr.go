package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// These options precede user options because OpenSSH uses the first value.
// A background command must never share the GUI's terminal for authentication.
func nonInteractiveSSHOptions(options []string) []string {
	result := []string{"-T", "-o", "BatchMode=yes", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no"}
	if passwordBroker != nil {
		// Forced askpass uses the in-memory password broker, never the GUI tty.
		result = []string{"-T", "-o", "BatchMode=no", "-o", "PasswordAuthentication=yes", "-o", "KbdInteractiveAuthentication=yes", "-o", "NumberOfPasswordPrompts=1"}
	}
	result = append(result, options...)
	// Legacy callers append remote commands directly. An exact capacity keeps
	// concurrent commands from overwriting each other's argument storage.
	return result[:len(result):len(result)]
}

func terminalSafeText(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	runes := []rune(strings.Join(strings.Fields(s), " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}

func fetchPLDMPDRs(ctx context.Context, options []string) ([]byte, int, error) {
	base := nonInteractiveSSHOptions(options)
	args := []string{"sudo", "-n", "pldmtool", "platform", "getpdr", "-a", "-n", "2"}
	for attempt := 0; attempt < 2; attempt++ {
		cmd := exec.CommandContext(ctx, "ssh", append(append([]string(nil), base...), args...)...)
		slog.Debug("Fetching PLDM PDRs", "attempt", attempt+1, "command", strings.Join(args, " "))
		cmd.WaitDelay = 2 * time.Second
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		runErr := cmd.Run()
		if ctx.Err() != nil {
			return nil, 0, fmt.Errorf("pldmtool getpdr: %w", ctx.Err())
		}
		var records []map[string]json.RawMessage
		parseErr := json.Unmarshal(stdout.Bytes(), &records)
		var fetchErr error
		detail := terminalSafeText(stderr.String(), 800)
		switch {
		case runErr != nil:
			fetchErr = fmt.Errorf("command failed: %w; stderr: %s", runErr, detail)
		case parseErr != nil:
			fetchErr = fmt.Errorf("invalid JSON: %w; stderr: %s; stdout (%d bytes): %q", parseErr, detail, stdout.Len(), terminalSafeText(stdout.String(), 160))
		case len(records) == 0:
			fetchErr = fmt.Errorf("empty PDR repository; stderr: %s", detail)
		default:
			for _, rec := range records {
				if len(rec["PDRType"]) == 0 {
					fetchErr = fmt.Errorf("record has no PDRType")
					break
				}
			}
		}
		// Never accept partial data from a failed command as a complete index.
		if fetchErr == nil {
			return stdout.Bytes(), len(records), nil
		}
		fetchErr = fmt.Errorf("command: %s: %w", strings.Join(args, " "), fetchErr)
		// Some releases report success despite returning incomplete output.
		// Retry once without -n whenever the first attempt is unusable.
		if attempt == 0 {
			slog.Debug("PLDM PDR attempt failed; retrying without -n 2", "error", fetchErr)
			args = args[:len(args)-2]
			continue
		}
		return nil, 0, fmt.Errorf("pldmtool getpdr: fallback without -n 2 failed; %w", fetchErr)
	}
	panic("unreachable")
}
