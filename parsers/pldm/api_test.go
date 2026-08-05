package pldm

import (
	"strings"
	"testing"
)

func TestExtractHexPrefixBoundsLargePayload(t *testing.T) {
	line := "host pldmd Rx: 80 02 51 34 12 " + strings.Repeat("AA ", 10000) + "trailer"
	got := ExtractHexPrefix(line, 5)
	if want := "80 02 51 34 12"; got != want {
		t.Fatalf("ExtractHexPrefix() = %q, want %q", got, want)
	}
}

func TestExtractHexPrefixStopsAtNonHex(t *testing.T) {
	if got := ExtractHexPrefix("pldmd Tx: 00 02 not-hex AA", 5); got != "00 02" {
		t.Fatalf("ExtractHexPrefix() = %q, want %q", got, "00 02")
	}
}

func TestExtractHexPrefixIgnoresFilterColoring(t *testing.T) {
	line := "pldmd \x1b[0;33mTx:\x1b[0m 80 02 \x1b[0;44m21\x1b[0m 34 12 FF"
	if got := ExtractHexPrefix(line, 5); got != "80 02 21 34 12" {
		t.Fatalf("ExtractHexPrefix() = %q, want %q", got, "80 02 21 34 12")
	}
}

func TestParsePayloadLimitsVariableFieldDisplay(t *testing.T) {
	payload := make([]byte, 1024)
	results := parsePayload(payload, []string{"RecordData:*"})
	if len(results) != 1 {
		t.Fatalf("parsePayload() returned %d results, want 1", len(results))
	}
	if !strings.Contains(results[0].Value, "960 bytes omitted") {
		t.Fatalf("variable payload was not truncated: %q", results[0].Value)
	}
}

func TestGetStateSensorReadingsSensorID(t *testing.T) {
	lines := []string{
		"2026-08-05T10:00:00Z pldmd Tx: 80 02 21 34 12 FF 00",
		"2026-08-05T10:00:01Z pldmd Rx: 00 02 21 00 01 01 02 03 04",
	}
	BuildLineIndex(lines)
	for _, selected := range []int{0, 1} {
		sensorID, ok := GetStateSensorReadingsSensorID(lines, selected)
		if !ok {
			t.Fatalf("GetStateSensorReadingsSensorID(lines, %d) did not resolve", selected)
		}
		if sensorID != 0x1234 {
			t.Fatalf("GetStateSensorReadingsSensorID(lines, %d) = %#x, want %#x", selected, sensorID, 0x1234)
		}
	}
}

func TestGetStateSensorReadingsSensorIDRejectsOtherCommands(t *testing.T) {
	lines := []string{"pldmd Tx: 80 02 20 34 12 01"}
	if _, ok := GetStateSensorReadingsSensorID(lines, 0); ok {
		t.Fatal("non-GetStateSensorReadings command unexpectedly resolved")
	}
}
