package main

import (
	"reflect"
	"testing"
)

func TestSSHJournalCmdAddsNonInteractiveSudoOnlyForRetry(t *testing.T) {
	app := &App{sshOptions: []string{"host", "-p", "2222"}}

	plain := app.sshJournalCmd(false, "journalctl", "-n", "1")
	wantPlain := []string{"ssh", "host", "-p", "2222", "journalctl", "-n", "1"}
	if !reflect.DeepEqual(plain.Args, wantPlain) {
		t.Fatalf("plain command args = %q, want %q", plain.Args, wantPlain)
	}

	withSudo := app.sshJournalCmd(true, "journalctl", "-n", "1")
	wantSudo := []string{"ssh", "host", "-p", "2222", "sudo", "-n", "journalctl", "-n", "1"}
	if !reflect.DeepEqual(withSudo.Args, wantSudo) {
		t.Fatalf("sudo command args = %q, want %q", withSudo.Args, wantSudo)
	}
}
