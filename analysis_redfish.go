package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

type redfishAuth struct {
	user     string
	password []byte
	baseURL  string
	insecure bool
}

func (a *redfishAuth) prompt() error {
	base, err := url.Parse(a.baseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return fmt.Errorf("--redfish-url must be an HTTPS base URL without embedded credentials")
	}
	if a.user != "" && a.password != nil {
		return nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("Redfish credentials require an interactive terminal: %w", err)
	}
	defer tty.Close()
	if strings.TrimSpace(a.user) == "" {
		fmt.Fprint(tty, "Redfish username: ")
		a.user, err = bufio.NewReader(tty).ReadString('\n')
		if err != nil {
			return err
		}
		a.user = strings.TrimSpace(a.user)
	}
	if a.user == "" {
		return fmt.Errorf("Redfish username is required")
	}
	if a.password != nil {
		return nil
	}
	fmt.Fprint(tty, "Redfish password: ")
	a.password, err = term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	return err
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (a *redfishAuth) curl() string {
	command := "curl --silent --show-error --fail --max-time 30"
	if a.insecure {
		command += " --insecure"
	}
	return command
}

type redfishSession struct {
	auth            *redfishAuth
	token, location string
}

func createRedfishSession(ctx context.Context, options []string, auth *redfishAuth) (*redfishSession, error) {
	if auth == nil {
		return nil, fmt.Errorf("Redfish username/password not configured")
	}
	payload, err := json.Marshal(struct {
		UserName string
		Password string
	}{auth.user, string(auth.password)})
	if err != nil {
		return nil, err
	}
	defer clear(payload)
	command := auth.curl() + " --dump-header - --request POST --header 'Content-Type: application/json' --data-binary @- " + shellQuote(strings.TrimRight(auth.baseURL, "/")+"/redfish/v1/SessionService/Sessions")
	data, err := queryVMIInput(ctx, options, command, payload)
	if err != nil {
		return nil, fmt.Errorf("Redfish login: %w", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid Redfish login response: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("Redfish login returned HTTP %d", response.StatusCode)
	}
	token, location := response.Header.Get("X-Auth-Token"), response.Header.Get("Location")
	if token == "" || location == "" {
		return nil, fmt.Errorf("Redfish session response missing X-Auth-Token or Location")
	}
	session := &redfishSession{auth: auth, token: token, location: location}
	if _, err := session.resourceURL(location); err != nil {
		return nil, err
	}
	return session, nil
}

// Keep credentials on the configured origin; never follow redirects with tokens.
func (s *redfishSession) resourceURL(path string) (string, error) {
	base, err := url.Parse(strings.TrimRight(s.auth.baseURL, "/") + "/")
	if err != nil {
		return "", err
	}
	resource, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	target := base.ResolveReference(resource)
	if target.Scheme != base.Scheme || target.Host != base.Host || target.User != nil || !strings.HasPrefix(target.Path, "/redfish/v1/") {
		return "", fmt.Errorf("Redfish resource must use the configured origin and /redfish/v1/ path")
	}
	return target.String(), nil
}

func (s *redfishSession) request(ctx context.Context, options []string, method, path string) ([]byte, error) {
	target, err := s.resourceURL(path)
	if err != nil {
		return nil, err
	}
	// Curl config on stdin prevents the token from appearing in process arguments.
	config := []byte("header = " + curlConfigQuote("X-Auth-Token: "+s.token) + "\n")
	defer clear(config)
	command := s.auth.curl() + " --config - --request " + method + " " + shellQuote(target)
	data, err := queryVMIInput(ctx, options, command, config)
	if err != nil {
		return nil, fmt.Errorf("Redfish %s: %s", method, strings.ReplaceAll(err.Error(), s.token, "[redacted]"))
	}
	return data, nil
}
func curlConfigQuote(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return "\"" + s + "\""
}

// SSH has already authenticated before analysis starts. Reuse only an account
// password, never an SSH key passphrase. Copy it so each owner can clear memory.
func (a *redfishAuth) reuseSSH(options []string, target string, broker *sshPasswordBroker) {
	user := ""
	if index := strings.LastIndex(target, "@"); index >= 0 {
		user = target[:index]
	}
	if user == "" {
		args := append([]string{"-G"}, options...)
		if config, err := exec.Command("ssh", args...).Output(); err == nil {
			for _, line := range strings.Split(string(config), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "user" {
					user = fields[1]
					break
				}
			}
		}
	}
	if user == "" {
		return
	}
	if a.user != "" && a.user != user {
		return
	}
	a.user = user
	if broker == nil {
		return
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	var password []byte
	count := 0
	for prompt, value := range broker.passwords {
		lower := strings.ToLower(prompt)
		if strings.Contains(lower, "password") && !strings.Contains(lower, "passphrase") {
			password = value
			count++
		}
	}
	if count == 1 {
		a.password = append([]byte{}, password...)
	}
}
