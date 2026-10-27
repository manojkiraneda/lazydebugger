package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSSHPasswordBrokerCachesOnlyKnownPrompts(t *testing.T) {
	for _, key := range []string{"SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "LAZYDEBUGGER_ASKPASS_SOCKET"} {
		t.Setenv(key, os.Getenv(key))
	}
	broker, err := startSSHPasswordBroker()
	if err != nil {
		t.Fatal(err)
	}
	defer broker.close()
	broker.passwords["service@bmc's password:"] = []byte("test-password")
	broker.seal()
	for _, tt := range []struct{ prompt, want string }{
		{"service@bmc's password:", "test-password"},
		{"other@bmc's password:", ""},
		{"service@other-bmc's password:", ""},
		{"Are you sure you want to continue connecting (yes/no)?", ""},
	} {
		conn, err := net.DialTimeout("unix", broker.listener.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		if err := json.NewEncoder(conn).Encode(tt.prompt); err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(conn)
		conn.Close()
		if err != nil || string(got) != tt.want {
			t.Fatalf("prompt %q: answer mismatch or error %v", tt.prompt, err)
		}
	}
	helper, err := os.ReadFile(os.Getenv("SSH_ASKPASS"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(helper), "test-password") {
		t.Fatal("password written to helper")
	}
	info, err := os.Stat(broker.dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("authentication directory is not private")
	}
	password := broker.passwords["service@bmc's password:"]
	broker.close()
	if _, err := os.Stat(broker.dir); !os.IsNotExist(err) {
		t.Fatal("authentication files not removed")
	}
	for _, b := range password {
		if b != 0 {
			t.Fatal("cached password was not cleared")
		}
	}
}

func TestSSHPasswordAuthenticationDoesNotUseTerminal(t *testing.T) {
	passwordBroker = &sshPasswordBroker{}
	defer func() { passwordBroker = nil }()
	args := nonInteractiveSSHOptions([]string{"service@bmc"})
	got := strings.Join(args, " ")
	for _, want := range []string{"BatchMode=no", "PasswordAuthentication=yes", "NumberOfPasswordPrompts=1", "KbdInteractiveAuthentication=yes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing authentication option %q", want)
		}
	}
	if strings.Contains(got, "BatchMode=yes") {
		t.Fatal("password authentication disabled")
	}
	if strings.Contains(got, "PreferredAuthentications=") {
		t.Fatal("authentication preference overrides user options")
	}
}
