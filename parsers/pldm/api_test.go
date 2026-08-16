package pldm

import (
	"reflect"
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

func TestDSP0247BIOSCommandsAreDefined(t *testing.T) {
	if err := loadPLDMSpec(); err != nil {
		t.Fatalf("loadPLDMSpec() error: %v", err)
	}

	want := map[uint8]string{
		0x01: "GetBIOSTable",
		0x02: "SetBIOSTable",
		0x03: "UpdateBIOSTable",
		0x04: "GetBIOSTableTags",
		0x05: "SetBIOSTableTags",
		0x06: "AcceptBIOSAttributesPendingValues",
		0x07: "SetBIOSAttributeCurrentValue",
		0x08: "GetBIOSAttributeCurrentValueByHandle",
		0x09: "GetBIOSAttributePendingValueByHandle",
		0x0A: "GetBIOSAttributeCurrentValueByType",
		0x0B: "GetBIOSAttributePendingValueByType",
		0x0C: "GetDateTime",
		0x0D: "SetDateTime",
		0x0E: "GetBIOSStringTableStringType",
		0x0F: "SetBIOSStringTableStringType",
	}

	for code, name := range want {
		definition := getCommandDef(0x03, code)
		if definition == nil {
			t.Errorf("BIOS command 0x%02X is not defined", code)
			continue
		}
		if definition.Name != name {
			t.Errorf("BIOS command 0x%02X name = %q, want %q", code, definition.Name, name)
		}
	}

	command08 := getCommandDef(0x03, 0x08)
	wantRequest := []string{"DataTransferHandle:4", "TransferOperationFlag:1", "AttributeHandle:2"}
	if command08 == nil || !reflect.DeepEqual(command08.Request, wantRequest) {
		t.Fatalf("BIOS command 0x08 request = %#v, want %#v", command08, wantRequest)
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

func TestSplitPossibleStates(t *testing.T) {
	got := splitPossibleStates([]string{"Unknown(0), Normal(1)", " Warning(2) ", ""})
	want := []string{"Unknown(0)", "Normal(1)", "Warning(2)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitPossibleStates() = %#v, want %#v", got, want)
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

func TestGetStateSensorPDRSensorIDForPlatformEventMessage(t *testing.T) {
	lines := []string{
		"2026-08-05T10:00:00Z pldmd Tx: 80 02 0A 01 08 00 34 12 01 00 01 00",
		"2026-08-05T10:00:01Z pldmd Rx: 00 02 0A 00 00",
	}
	BuildLineIndex(lines)
	for _, selected := range []int{0, 1} {
		sensorID, ok := GetStateSensorPDRSensorID(lines, selected)
		if !ok {
			t.Fatalf("GetStateSensorPDRSensorID(lines, %d) did not resolve", selected)
		}
		if sensorID != 0x1234 {
			t.Fatalf("GetStateSensorPDRSensorID(lines, %d) = %#x, want %#x", selected, sensorID, 0x1234)
		}
	}
}

func TestGetStateSensorPDRSensorIDRejectsNonSensorPlatformEvent(t *testing.T) {
	lines := []string{"pldmd Tx: 80 02 0A 01 08 04 34 12 01"}
	if _, ok := GetStateSensorPDRSensorID(lines, 0); ok {
		t.Fatal("non-sensor PlatformEventMessage unexpectedly resolved a SensorID")
	}
}
