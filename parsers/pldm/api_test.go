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

func TestReceiveEnvelopeAndBareHostMessages(t *testing.T) {
	request := "80 02 51 00 00 00 00 00 00 00 00 01 ff ff 00 00"
	response := "00 02 51 00 02 00 00 00 00 00 00 00 05 14 00 01 00 00 00 01 14 00 00 0a 00 01 00 01 00 01 2d 01 00 00 00"
	newResponse := "00 02 51 00 16 00 00 02 00 00 00 00 05 1e 00 15 00 00 02 01 0b 01 00 14 00 00 d0 10 00 a6 80 10 00 01 80 00 00 00 00 01 02 00 02 00 02"
	for _, tt := range []struct{ name, line, want string }{
		{"received tool request", "Oct 26 02:14:23 p11bmc pldmd[1422]: Rx: 08 01 " + request, request},
		{"transmitted response", "Oct 26 02:14:23 p11bmc pldmd[1422]: Tx: " + response, response},
		{"received prefixed response", "Oct 26 02:55:48 p11bmc pldmd[1422]: Rx: 08 01 " + newResponse, newResponse},
		{"transmitted matching response", "Oct 26 02:55:48 p11bmc pldmd[1422]: Tx: " + newResponse, newResponse},
		{"bare host request", "pldmd Rx: " + request, request},
		{"bare BMC request", "pldmd Tx: " + request, request},
		{"bare host response", "pldmd Rx: " + response, response},
		{"bare instance 8", "pldmd Rx: 08 02 51 00", "08 02 51 00"},
		{"short unrelated message", "pldmd Rx: 08 01 00 00", "08 01 00 00"},
		{"transmitted bytes untouched", "pldmd Tx: 08 01 " + request, "08 01 " + request},
		{"colored prefix", "pldmd \x1b[33mRx:\x1b[0m \x1b[44m08\x1b[0m 01 " + request, request},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractHexBytes(tt.line); got != tt.want {
				t.Fatalf("extracted %q, want %q", got, tt.want)
			}
			for _, limit := range []int{1, 2, 3, 5, 8} {
				fields := strings.Fields(tt.want)
				want := strings.Join(fields[:min(limit, len(fields))], " ")
				if got := ExtractHexPrefix(tt.line, limit); got != want {
					t.Fatalf("prefix(%d) = %q, want %q", limit, got, want)
				}
			}
		})
	}
	lines := []string{
		"Oct 26 02:14:23 p11bmc pldmd[1422]: Rx: 08 01 " + request,
		"Oct 26 02:14:23 p11bmc pldmd[1422]: Tx: " + response,
	}
	BuildLineIndex(lines)
	for i := range lines {
		if got := FindCorrelatedLine(lines, i); got != 1-i {
			t.Fatalf("correlation from %d = %d", i, got)
		}
		if !MatchesSemanticPatterns(ExtractHexPrefix(lines[i], 5), []string{"02 51"}) {
			t.Fatal("GetPDR semantic filter failed")
		}
	}
	bytes, err := hexStringToBytes(ExtractHexBytes(lines[0]))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := parsePLDMMessage(bytes)
	if err != nil || !msg.IsRequest || msg.InstanceID != 0 || msg.PLDMType != 2 || msg.CommandCode != 0x51 {
		t.Fatalf("GetPDR header = %+v, %v", msg, err)
	}
}

func TestReceiveEnvelopeSensorLookup(t *testing.T) {
	lines := []string{
		"2026-10-26T02:14:23Z pldmd Rx: 08 01 80 02 21 34 12 00 00",
		"2026-10-26T02:14:24Z pldmd Tx: 00 02 21 00 01 01 02 03 04",
	}
	BuildLineIndex(lines)
	for i := range lines {
		id, ok := GetStateSensorReadingsSensorID(lines, i)
		if !ok || id != 0x1234 {
			t.Fatalf("sensor lookup from %d = %x, %v", i, id, ok)
		}
	}
}

func TestReadFileByTypeResponseGetsTypeFromRequest(t *testing.T) {
	lines := []string{
		"2026-10-26T02:47:40Z pldmd Tx: 80 3F 0B 03 00 34 12 00 00 00 00 00 00 04 00 00 00",
		"2026-10-26T02:47:41Z pldmd Rx: 08 01 00 3F 0B 00 04 00 00 00 DE AD BE EF",
	}
	BuildLineIndex(lines)
	if !IsReadFileByTypeResponse(lines[1]) || IsReadFileByTypeResponse(lines[0]) {
		t.Fatal("response direction misidentified")
	}
	for i := range lines {
		ft, ok := GetReadFileByTypeFileType(lines, i)
		if !ok || ft != 3 {
			t.Fatalf("FileType from line %d = %#x, %v", i, ft, ok)
		}
	}
	if _, ok := GetReadFileByTypeFileType(lines[1:], 0); ok {
		t.Fatal("response without its request gained a FileType")
	}
}
