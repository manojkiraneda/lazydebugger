package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Only the private socket path is inherited by SSH helpers. Passwords stay in
// this process and travel over a socket inside a user-only temporary directory.
type sshPasswordBroker struct {
	mu        sync.Mutex
	passwords map[string][]byte
	prompting bool
	listener  net.Listener
	dir       string
}

var passwordBroker *sshPasswordBroker

func startSSHPasswordBroker() (*sshPasswordBroker, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "lazydebugger-auth-")
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "socket"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	broker := &sshPasswordBroker{passwords: make(map[string][]byte), prompting: true, listener: listener, dir: dir}
	helper := filepath.Join(dir, "askpass")
	// Quote the executable path for POSIX sh; no credentials are written here.
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' --internal-ssh-askpass \"$@\"\n"
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		broker.close()
		return nil, err
	}
	for key, value := range map[string]string{"SSH_ASKPASS": helper, "SSH_ASKPASS_REQUIRE": "force", "LAZYDEBUGGER_ASKPASS_SOCKET": listener.Addr().String()} {
		if err := os.Setenv(key, value); err != nil {
			broker.close()
			return nil, err
		}
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go broker.answer(conn)
		}
	}()
	return broker, nil
}

func (b *sshPasswordBroker) answer(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Minute))
	var prompt string
	if json.NewDecoder(io.LimitReader(conn, 8192)).Decode(&prompt) != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if password, ok := b.passwords[prompt]; ok {
		conn.Write(password)
		return
	}
	// New prompts are allowed only during startup, before the GUI owns the tty.
	if !b.prompting {
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt+" ")
	lowerPrompt := strings.ToLower(prompt)
	if !strings.Contains(lowerPrompt, "are you sure you want to continue connecting") {
		password, err := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(tty)
		if err != nil {
			return
		}
		if strings.Contains(lowerPrompt, "password") || strings.Contains(lowerPrompt, "passphrase") {
			b.passwords[prompt] = password
		}
		conn.Write(password)
	} else {
		// Host-key confirmation is interactive and must never be cached or accepted
		// automatically.
		answer, err := bufio.NewReader(tty).ReadString('\n')
		if err == nil {
			io.WriteString(conn, strings.TrimRight(answer, "\r\n"))
		}
	}
}

func (b *sshPasswordBroker) seal() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prompting = false
}

func (b *sshPasswordBroker) close() {
	b.listener.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	for prompt, password := range b.passwords {
		clear(password)
		delete(b.passwords, prompt)
	}
	os.RemoveAll(b.dir)
}

func runSSHAskpass() int {
	conn, err := net.DialTimeout("unix", os.Getenv("LAZYDEBUGGER_ASKPASS_SOCKET"), 5*time.Second)
	if err != nil {
		return 1
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Minute))
	if len(os.Args) < 3 {
		return 1
	}
	if json.NewEncoder(conn).Encode(os.Args[2]) != nil {
		return 1
	}
	answer, err := io.ReadAll(io.LimitReader(conn, 8192))
	if err != nil || len(answer) == 0 {
		return 1
	}
	_, err = os.Stdout.Write(answer)
	if err != nil {
		return 1
	}
	return 0
}
