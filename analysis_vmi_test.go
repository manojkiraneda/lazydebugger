package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseBusctlValues(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{`s "0.0.0.0"`, "0.0.0.0"}, {`s ""`, ""}, {"y 0", "0"}, {"y 128", "128"}, {"b false", "false"}, {"b true", "true"}} {
		got, err := parseBusctlValue(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q %v", tc.input, got, err)
		}
	}
	for _, input := range []string{"", "s unquoted", "y 256", "b broken"} {
		if _, err := parseBusctlValue(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestParseRedfishValues(t *testing.T) {
	data := []byte(`{"Id":"eth0","InterfaceEnabled":false,"IPv4Addresses":[{"Address":"0.0.0.0","Gateway":"0.0.0.0","SubnetMask":"0.0.0.0","AddressOrigin":"Static"}],"IPv6Addresses":[{"Address":"::","PrefixLength":0,"AddressOrigin":"DHCPv6"}],"IPv6DefaultGateway":"::"}`)
	values, err := parseRedfishVMI(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, value := range values {
		found[value.attribute] = value.value
	}
	for name, want := range map[string]string{"vmi_if0_ipv4_ipaddr": "0.0.0.0", "vmi_if0_ipv4_prefix_length": "0", "vmi_if0_ipv6_gateway": "::", "vmi_if0_ipv6_prefix_length": "0", "vmi_if0_ipv4_interface_enabled": "false"} {
		if found[name] != want {
			t.Errorf("%s = %q, want %q", name, found[name], want)
		}
	}
	values, err = parseRedfishVMI([]byte(`{"Id":"eth1"}`), 1)
	if err != nil || values[0].value != "Not reported" {
		t.Fatalf("missing properties: %v %v", values, err)
	}
}

func TestVMIStageOrderAndFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake SSH")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$*" >> "$VMI_TEST_CALLS"` + "\n" +
		`cat >> "$VMI_TEST_INPUTS"` + "\n" +
		"case \"$*\" in\n" +
		" *curl*POST*) cat >/dev/null; printf 'HTTP/1.1 201 Created\\r\\nX-Auth-Token: test-session-token\\r\\nLocation: /redfish/v1/SessionService/Sessions/test\\r\\nContent-Length: 2\\r\\n\\r\\n{}';;\n" +
		" *curl*DELETE*) cat >/dev/null;;\n" +
		" *curl*) echo '{\"Id\":\"eth0\",\"IPv4Addresses\":[{\"Address\":\"192.0.2.5\",\"SubnetMask\":\"255.255.255.0\"}]}';;\n" +
		" *busctl*Enabled*) echo 'b false';;\n" +
		" *busctl*PrefixLength*) echo 'y 0';;\n" +
		" *busctl*Gateway*) echo 'missing gateway' >&2; exit 1;;\n" +
		" *busctl*) echo 's \"0.0.0.0\"';;\n" +
		" *pldmtool*) echo '{\"CurrentValue\":0}';;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "calls")
	inputs := filepath.Join(dir, "inputs")
	t.Setenv("VMI_TEST_INPUTS", inputs)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VMI_TEST_CALLS", calls)
	plugins, err := loadAnalysisPlugins("")
	if err != nil {
		t.Fatal(err)
	}
	plugins[0].auth = &redfishAuth{user: "test-user", password: []byte("test-password"), baseURL: "https://localhost"}
	var out bytes.Buffer
	err = runAnalysisCommands(context.Background(), []string{"bmc"}, plugins[0], &out)
	if err == nil {
		t.Fatal("missing failure summary")
	}
	log, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	// 4 Redfish (POST+2×GET+DELETE) + 20 D-Bus + 16 BIOS = 40.
	// PHYP now goes through the Go vtty client, not the fake ssh binary.
	if len(lines) != 40 {
		t.Fatalf("got %d SSH commands, want 4 Redfish + 20 D-Bus + 16 BIOS", len(lines))
	}
	counts := map[string]int{}
	for i, line := range lines {
		switch {
		case strings.Contains(line, "curl"):
			counts["curl"]++
		case strings.Contains(line, "busctl"):
			counts["busctl"]++
		case strings.Contains(line, "pldmtool"):
			counts["pldmtool"]++
		default:
			t.Errorf("query %d unrecognised: %s", i, line)
		}
	}
	// Redfish must come first (lines 0-3 are curl).
	for i := 0; i < 4; i++ {
		if !strings.Contains(lines[i], "curl") {
			t.Errorf("query %d: expected curl (Redfish): %s", i, lines[i])
		}
	}
	// D-Bus and BIOS run in parallel after Redfish — count only.
	if counts["curl"] != 4 || counts["busctl"] != 20 || counts["pldmtool"] != 16 {
		t.Errorf("wrong command counts: %v", counts)
	}
	input, err := os.ReadFile(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(input), `"UserName":"test-user"`) || !strings.Contains(string(input), `"Password":"test-password"`) || !strings.Contains(string(input), "X-Auth-Token: test-session-token") {
		t.Fatal("login credentials or token were not sent through SSH stdin")
	}
	for _, secret := range []string{"test-session-token", "test-password"} {
		if strings.Contains(string(log), secret) || strings.Contains(out.String(), secret) {
			t.Fatal("secret exposed in commands or displayed report")
		}
	}
	report := out.String()
	if strings.Contains(report, "busctl ") {
		t.Fatal("D-Bus report must show values without command text")
	}
	header := report[strings.Index(report, "PROPERTY"):]
	r, d, b := strings.Index(header, "1. REDFISH"), strings.Index(header, "2. D-BUS"), strings.Index(header, "3. BIOS ATTRIBUTES")
	if r < 0 || d <= r || b <= d {
		t.Fatalf("incorrect report order: %s", report)
	}
	for _, text := range []string{"192.0.2.5", "Enabled", "missing gateway", "Interface 1 / IPV6"} {
		if !strings.Contains(report, text) {
			t.Errorf("missing %q in report", text)
		}
	}
}

func TestRedfishBMCSample(t *testing.T) {
	for iface := 0; iface < 2; iface++ {
		sample := []byte(fmt.Sprintf(`{
    "@odata.id":"/redfish/v1/Systems/hypervisor/EthernetInterfaces/eth%d",
    "@odata.type":"#EthernetInterface.v1_8_0.EthernetInterface",
    "Id":"eth%d","InterfaceEnabled":false,
    "DHCPv4":{"DHCPEnabled":false},"DHCPv6":{"OperatingMode":"Disabled"},
    "IPv4Addresses":[{"Address":"0.0.0.0","AddressOrigin":"Static","Gateway":"0.0.0.0","SubnetMask":"0.0.0.0"}],
    "IPv4StaticAddresses":[{"Address":"0.0.0.0","AddressOrigin":"Static","Gateway":"0.0.0.0","SubnetMask":"0.0.0.0"}],
    "IPv6Addresses":[{"Address":"::","AddressOrigin":"Static","PrefixLength":128}],
    "IPv6StaticAddresses":[{"Address":"::","PrefixLength":128}],
    "IPv6DefaultGateway":"::","IPv6StaticDefaultGateways":[],
    "StatelessAddressAutoConfig":{"IPv6AutoConfigEnabled":false}
  }`, iface, iface))
		values, err := parseRedfishVMI(sample, iface)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, value := range values {
			got[value.attribute] = value.value
		}
		for suffix, want := range map[string]string{
			"ipv4_ipaddr": "0.0.0.0", "ipv4_gateway": "0.0.0.0", "ipv4_prefix_length": "0", "ipv4_method": "Static",
			"ipv4_dhcp_enabled": "false", "ipv4_interface_enabled": "false",
			"ipv6_ipaddr": "::", "ipv6_gateway": "::", "ipv6_prefix_length": "128", "ipv6_method": "Static",
			"ipv6_dhcp_mode": "Disabled", "ipv6_autoconfig": "false", "ipv6_interface_enabled": "false",
		} {
			key := fmt.Sprintf("vmi_if%d_%s", iface, suffix)
			if got[key] != want {
				t.Errorf("%s = %q, want %q", key, got[key], want)
			}
		}
	}
}
