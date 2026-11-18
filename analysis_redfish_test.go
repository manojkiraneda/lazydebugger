package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedfishSessionResourceOrigin(t *testing.T) {
	session := &redfishSession{auth: &redfishAuth{baseURL: "https://localhost"}, token: "test-token"}
	for _, path := range []string{"/redfish/v1/Systems/hypervisor/EthernetInterfaces/eth0", "https://localhost/redfish/v1/SessionService/Sessions/1"} {
		if _, err := session.resourceURL(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	for _, path := range []string{"https://other-host/redfish/v1/Systems", "http://localhost/redfish/v1/Systems", "https://user@localhost/redfish/v1/Systems", "/unrelated"} {
		if _, err := session.resourceURL(path); err == nil {
			t.Errorf("accepted token destination %s", path)
		}
	}
	if _, err := createRedfishSession(context.Background(), nil, nil); err == nil {
		t.Fatal("accepted missing credentials")
	}
}

func TestLegacyVMIConfigIncludesAllStages(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "legacy.yml")
	if err := os.WriteFile(filename, []byte("plugins:\n  - name: vmi\n    output: bios-values\n    commands: [pldmtool]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plugins, err := loadAnalysisPlugins(filename)
	if err != nil {
		t.Fatal(err)
	}
	if plugins[0].Output != "vmi-network" || len(plugins[0].RedfishPaths) != 2 {
		t.Fatal("default VMI must include authenticated Redfish, D-Bus, and BIOS")
	}
	for _, path := range plugins[0].RedfishPaths {
		if !strings.HasPrefix(path, "/redfish/v1/") {
			t.Fatal("unexpected Redfish path")
		}
	}
}

func TestRedfishReusesSSHLogin(t *testing.T) {
	broker := &sshPasswordBroker{passwords: map[string][]byte{"root@bmc's password:": []byte("login-secret")}}
	auth := redfishAuth{baseURL: "https://localhost", insecure: true}
	auth.reuseSSH(nil, "root@bmc", broker)
	if auth.user != "root" || string(auth.password) != "login-secret" {
		t.Fatal("SSH login was not reused")
	}
	if err := auth.prompt(); err != nil {
		t.Fatalf("cached login must not prompt: %v", err)
	}
	if !strings.Contains(auth.curl(), "--insecure") {
		t.Fatal("self-signed certificate option not applied")
	}
	clear(auth.password)
	if string(broker.passwords["root@bmc's password:"]) != "login-secret" {
		t.Fatal("reused password must be an independent copy")
	}
	for _, tc := range []struct {
		name, user string
		passwords  map[string][]byte
	}{
		{"key passphrase", "", map[string][]byte{"Enter passphrase for key:": []byte("key-secret")}},
		{"different user", "other", broker.passwords},
		{"multiple accounts", "", map[string][]byte{"root@bmc's password:": []byte("one"), "jump@proxy's password:": []byte("two")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := redfishAuth{user: tc.user}
			auth.reuseSSH(nil, "root@bmc", &sshPasswordBroker{passwords: tc.passwords})
			if auth.password != nil {
				t.Fatal("reused an unrelated or ambiguous secret")
			}
		})
	}
}
