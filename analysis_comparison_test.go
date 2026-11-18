package main

import (
	"strings"
	"testing"
)

func TestVMIComparisonWaitsForEverySource(t *testing.T) {
	report := &vmiComparison{stage: 1, active: -1, complete: [3]bool{true, false, true}}
	if strings.Contains(report.format(), "Checks complete") {
		t.Fatal("BIOS completion must not hide pending D-Bus checks")
	}
	report.complete[1] = true
	if !strings.Contains(report.format(), "Checks complete") {
		t.Fatal("completed sources did not finish the report")
	}
}

func TestVMIComparisonKeepsPublishedSnapshot(t *testing.T) {
	values := []biosDisplayValue{{attribute: "vmi_if0_ipv4_ipaddr", value: "192.0.2.1", done: true}}
	report := &vmiComparison{}
	report.publish(2, values, -1)
	values[0].value = "192.0.2.2"
	if report.values[2][0].value != "192.0.2.1" {
		t.Fatal("worker mutations changed the published comparison snapshot")
	}
	report.publish(2, values, -1)
	if report.values[2][0].value != "192.0.2.2" {
		t.Fatal("publishing did not update the comparison snapshot")
	}
}

func TestVMIComparisonAlignsSources(t *testing.T) {
	report := &vmiComparison{stage: 2, active: -1}
	report.values[0] = []biosDisplayValue{{attribute: "vmi_if0_ipv6_ipaddr", value: "2001:db8:1234:5678:abcd:ef01:2345:6789", done: true}}
	report.values[1] = []biosDisplayValue{{attribute: "vmi_if0_ipv6_ipaddr", value: "::", done: true}, {attribute: "vmi_if0_ipv6_enabled", value: "false", done: true}}
	report.values[2] = []biosDisplayValue{{attribute: "vmi_if0_ipv6_ipaddr", value: "::", done: true}}
	text := report.format()
	if strings.Contains(text, "busctl") || strings.Contains(text, "pldmtool") {
		t.Fatal("comparison must contain values, not commands")
	}
	lines := strings.Split(text, "\n")
	header := -1
	for row, line := range lines {
		if strings.HasPrefix(line, "PROPERTY") {
			header = row
			if !strings.Contains(line, "1. REDFISH") || !strings.Contains(line, "2. D-BUS") || !strings.Contains(line, "3. BIOS ATTRIBUTES") {
				t.Fatal("sources must share one header row")
			}
		}
		if strings.Contains(line, "2001:db8:") {
			// 4 columns: PROPERTY │ REDFISH │ D-BUS │ BIOS
			cells := strings.Split(line, "│")
			if len(cells) != 4 || strings.TrimSpace(cells[1]) != "2001:db8:1234:5678:abcd:ef01:2345:6789" || strings.TrimSpace(cells[2]) != "::" || strings.TrimSpace(cells[3]) != "::" {
				t.Fatalf("misaligned source values: %s", line)
			}
		}
	}
	if header < 0 {
		t.Fatal("missing header")
	}
	if len(lines) > 33 {
		t.Fatalf("main comparison too tall: %d lines", len(lines))
	}
}
