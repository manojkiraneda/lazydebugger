package pldm

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/awesome-gocui/gocui"
)

// ParseAndDisplay parses PLDM hex bytes and displays them in the given view
func ParseAndDisplay(dockerView *gocui.View, hexBytes string, frameColor, titleColor gocui.Attribute) error {
	// Load PLDM spec if not already loaded
	if pldmSpec == nil {
		if err := loadPLDMSpec(); err != nil {
			dockerView.Clear()
			fmt.Fprintf(dockerView, "\nError loading PLDM spec: %v\n", err)
			fmt.Fprintln(dockerView, "\nMake sure parsers/pldm/commands.yml exists")
			return err
		}
	}

	// Clear and update the docker panel
	dockerView.Clear()
	dockerView.SetOrigin(0, 0) //nolint:errcheck
	dockerView.Title = " PLDM Message Parser "
	dockerView.FrameColor = frameColor
	dockerView.TitleColor = titleColor

	if hexBytes == "" {
		fmt.Fprintln(dockerView, "")
		fmt.Fprintln(dockerView, "No PLDM hex data found in selected line")
		fmt.Fprintln(dockerView, "")
		fmt.Fprintln(dockerView, "Make sure the line contains 'Rx:' or 'Tx:' with hex data")
		return nil
	}

	// Parse hex string to bytes
	bytes, err := hexStringToBytes(hexBytes)
	if err != nil {
		fmt.Fprintln(dockerView, "")
		fmt.Fprintf(dockerView, "Error parsing hex: %v\n", err)
		return nil
	}

	// Display total byte count
	fmt.Fprintln(dockerView, "")
	fmt.Fprintf(dockerView, "Total: %d bytes\n", len(bytes))
	fmt.Fprintln(dockerView, "")

	// Parse PLDM message
	msg, err := parsePLDMMessage(bytes)
	if err != nil {
		fmt.Fprintf(dockerView, "Error: %v\n", err)
		return nil
	}
	if msg.IsRequest && msg.PLDMType == 0x3F && msg.CommandCode == 0x0B && len(bytes) >= 5 {
		fileType := uint16(bytes[3]) | uint16(bytes[4])<<8
		if name, ok := LookupFileTypeName(fileType); ok {
			fmt.Fprintf(dockerView, "  FileType: 0x%04X (%s)\n\n", fileType, name)
		} else {
			fmt.Fprintf(dockerView, "  FileType: 0x%04X\n\n", fileType)
		}
	}

	// Display message info with nice formatting
	fmt.Fprintln(dockerView, "")
	fmt.Fprintln(dockerView, "╔═══════════════════════════════════════════════╗")
	fmt.Fprintln(dockerView, "║           PLDM Message Header                 ║")
	fmt.Fprintln(dockerView, "╚═══════════════════════════════════════════════╝")
	fmt.Fprintln(dockerView, "")

	msgType := "Request"
	if !msg.IsRequest {
		msgType = "Response"
	}
	fmt.Fprintf(dockerView, "  Message Type:      %s\n", msgType)
	fmt.Fprintf(dockerView, "  Instance ID:       %d\n", msg.InstanceID)
	fmt.Fprintf(dockerView, "  Datagram:          %v\n", msg.Datagram)
	fmt.Fprintf(dockerView, "  Header Version:    %d\n", msg.HeaderVersion)

	// Get type name
	typeKey := fmt.Sprintf("0x%02X", msg.PLDMType)
	typeName := "Unknown"
	if pldmSpec != nil {
		if name, ok := pldmSpec.Types[typeKey]; ok {
			typeName = name
		}
	}
	fmt.Fprintf(dockerView, "  PLDM Type:         0x%02X (%s)\n", msg.PLDMType, typeName)

	// Get command info
	cmdDef := getCommandDef(msg.PLDMType, msg.CommandCode)
	cmdName := "Unknown"
	if cmdDef != nil {
		cmdName = cmdDef.Name
	}
	fmt.Fprintf(dockerView, "  Command Code:      0x%02X (%s)\n", msg.CommandCode, cmdName)
	fmt.Fprintln(dockerView, "")

	// Parse payload (after 3-byte header)
	if len(bytes) > 3 {
		payload := bytes[3:]
		fmt.Fprintln(dockerView, "╔═══════════════════════════════════════════════╗")
		fmt.Fprintln(dockerView, "║              Payload Data                     ║")
		fmt.Fprintln(dockerView, "╚═══════════════════════════════════════════════╝")
		fmt.Fprintln(dockerView, "")

		if cmdDef != nil {
			var fields []string
			var variantMap map[string]map[string]VariantBranch
			payloadOffset := 3 // absolute byte offset of payload[0]

			if msg.IsRequest {
				fields = cmdDef.Request
				variantMap = cmdDef.RequestVariants
			} else {
				fields = cmdDef.Response
				variantMap = cmdDef.ResponseVariants

				// For responses, peel off and display the completion code first.
				if len(payload) > 0 {
					ccKey := fmt.Sprintf("0x%02X", payload[0])
					ccName := "Unknown"
					if pldmSpec != nil {
						if name, ok := pldmSpec.CompletionCodes[ccKey]; ok {
							ccName = name
						}
					}
					fmt.Fprintf(dockerView, "  [%d] CompletionCode: 0x%02X (%s)\n", payloadOffset, payload[0], ccName)

					if len(payload) > 1 {
						payload = payload[1:]
						payloadOffset++
						if len(fields) > 0 && strings.HasPrefix(fields[0], "CC:") {
							fields = fields[1:]
						}
					} else {
						payload = nil
					}
				}
			}

			if len(fields) > 0 && len(payload) > 0 {
				// Use the variant-aware decoder when variants are present,
				// otherwise fall back to the original flat decoder.
				if variantMap != nil {
					results := ParseVariantPayload(payload, payloadOffset, fields, variantMap)
					displayVariantResults(dockerView, results)
				} else {
					parsedFields := parsePayload(payload, fields)
					for _, result := range parsedFields {
						var start, end int
						fmt.Sscanf(result.ByteRange, "[%d:%d]", &start, &end)
						adjustedRange := fmt.Sprintf("[%d:%d]", payloadOffset+start, payloadOffset+end)
						if result.Field == "RecordData" && result.Error == "" &&
							msg.PLDMType == 0x02 && msg.CommandCode == 0x51 &&
							start >= 0 && end >= start && end < len(payload) {
							record := payload[start : end+1]
							fmt.Fprintf(dockerView, "  %s RecordData (%d bytes):\n", adjustedRange, len(record))
							for i := 0; i < len(record); i += 12 {
								chunk := record[i:min(i+12, len(record))]
								fmt.Fprintf(dockerView, "    [%d] %s\n", payloadOffset+start+i, formatHexBytes(chunk))
							}
							continue
						}

						displayValue := result.Value
						fileName := ""
						if result.Error == "" && result.Field == "FileNameHandle" && msg.IsRequest &&
							msg.PLDMType == 0x3F && msg.CommandCode >= 0x04 && msg.CommandCode <= 0x07 &&
							start >= 0 && start+4 <= len(payload) {
							handle := binary.LittleEndian.Uint32(payload[start : start+4])
							if name, ok := LookupFileName(handle); ok {
								fileName = name
							}
						}

						if result.Error == "" && result.Field == "FileType" && msg.PLDMType == 0x3F {
							var hexVal uint16
							if n, _ := fmt.Sscanf(result.Value, "0x%04X", &hexVal); n == 1 {
								if name, ok := LookupFileTypeName(hexVal); ok {
									displayValue = fmt.Sprintf("%s (%s)", result.Value, name)
								}
							}
						}
						if result.Error != "" {
							fmt.Fprintf(dockerView, "  %s %s: %s ⚠️ ERROR: %s\n",
								adjustedRange, result.Field, displayValue, result.Error)
						} else {
							fmt.Fprintf(dockerView, "  %s %s: %s\n",
								adjustedRange, result.Field, displayValue)
						}
						if fileName != "" {
							fmt.Fprintf(dockerView, "    FileName: %s\n", fileName)
						}
					}
				}
			} else if len(fields) > 0 && len(payload) == 0 {
				fmt.Fprintln(dockerView, "  ⚠️ No payload data (expected fields)")
			}
		} else {
			// No command definition — show raw bytes with positions.
			for i, b := range payload {
				fmt.Fprintf(dockerView, "  [%d] 0x%02X (%d)\n", 3+i, b, b)
			}
		}
	} else {
		fmt.Fprintln(dockerView, "No payload data")
	}

	fmt.Fprintln(dockerView, "")

	return nil
}

// ParseAndDisplayWithStateSensorPDR renders a PLDM message and appends the
// indexed State Sensor PDR for sensorID. It is used for commands whose response
// does not echo the SensorID, such as GetStateSensorReadings.
func ParseAndDisplayWithStateSensorPDR(dockerView *gocui.View, hexBytes string, sensorID uint16, frameColor, titleColor gocui.Attribute) error {
	if err := ParseAndDisplay(dockerView, hexBytes, frameColor, titleColor); err != nil {
		return err
	}
	if !displayStateSensorPDR(dockerView, sensorID) {
		fmt.Fprintln(dockerView, "")
		fmt.Fprintf(dockerView, "State Sensor PDR for SensorID %d was not found in the loaded PDR index.\n", sensorID)
	}
	return nil
}

// displayVariantResults renders a []VariantResult into dockerView. PDR
// enrichment is performed by ParseAndDisplayWithStateSensorPDR after the
// selected request/response has been resolved to its SensorID.
func displayVariantResults(dockerView *gocui.View, results []VariantResult) {
	for _, r := range results {
		displayValue := r.Value
		if r.Error != "" {
			fmt.Fprintf(dockerView, "  %s %s: %s ⚠️ ERROR: %s\n",
				r.ByteRange, r.Field, displayValue, r.Error)
			continue
		}

		// Annotate known enum fields with human-readable names.
		switch r.Field {
		case "EventClass":
			displayValue = annotateEnum(r.RawValue, eventClassNames)
		case "SensorEventClass":
			displayValue = annotateEnum(r.RawValue, sensorEventClassNames)
		case "PresentOpState", "PreviousOpState":
			displayValue = annotateEnum(r.RawValue, sensorOpStateNames)
		case "SensorDataSize":
			displayValue = annotateEnum(r.RawValue, sensorDataSizeNames)
		}

		fmt.Fprintf(dockerView, "  %s %s: %s\n", r.ByteRange, r.Field, displayValue)
	}

}

func displayStateSensorPDR(dockerView *gocui.View, sensorID uint16) bool {
	pdr := LookupStateSensorPDR(sensorID)
	if pdr == nil {
		return false
	}
	fmt.Fprintln(dockerView, "")
	fmt.Fprintln(dockerView, "╔═══════════════════════════════════════════════╗")
	title := fmt.Sprintf("  State Sensor PDR  (SensorID %d)", sensorID)
	fmt.Fprintf(dockerView, "║%-47s║\n", title)
	fmt.Fprintln(dockerView, "╚═══════════════════════════════════════════════╝")
	fmt.Fprintln(dockerView, "")
	fmt.Fprintf(dockerView, "  Entity:     %s\n", pdr.EntityType)
	fmt.Fprintf(dockerView, "  Instance:   %d   ContainerID: %d\n",
		pdr.EntityInstanceNumber, pdr.ContainerID)
	fmt.Fprintf(dockerView, "  Composites: %d state sensor(s)\n", pdr.CompositeSensorCount)
	for i := 0; i < pdr.CompositeSensorCount; i++ {
		setName := ""
		if i < len(pdr.StateSetIDs) {
			setName = pdr.StateSetIDs[i]
		}
		fmt.Fprintf(dockerView, "  [%d] StateSet: %s\n", i, setName)
		if i < len(pdr.PossibleStates) {
			states := splitPossibleStates(pdr.PossibleStates[i])
			if len(states) > 0 {
				fmt.Fprintln(dockerView, "      States:")
				for _, state := range states {
					fmt.Fprintf(dockerView, "        - %s\n", state)
				}
			}
		}
	}
	return true
}

func splitPossibleStates(values []string) []string {
	states := make([]string, 0, len(values))
	for _, value := range values {
		for _, state := range strings.Split(value, ",") {
			if state = strings.TrimSpace(state); state != "" {
				states = append(states, state)
			}
		}
	}
	return states
}

// GetStateSensorPDRSensorID resolves the SensorID used to enrich commands with
// a State Sensor PDR. It supports GetStateSensorReadings and SensorEvent
// PlatformEventMessage requests. Responses obtain the ID from their correlated
// request because neither response echoes it.
func GetStateSensorPDRSensorID(lines []string, selected int) (uint16, bool) {
	if selected < 0 || selected >= len(lines) {
		return 0, false
	}
	messageBytes, err := hexStringToBytes(ExtractHexPrefix(lines[selected], 8))
	if err != nil || len(messageBytes) < 3 {
		return 0, false
	}
	msg, err := parsePLDMMessage(messageBytes)
	if err != nil || msg.PLDMType != 0x02 || (msg.CommandCode != 0x0A && msg.CommandCode != 0x21) {
		return 0, false
	}
	if !msg.IsRequest {
		selected = FindCorrelatedLine(lines, selected)
		if selected < 0 {
			return 0, false
		}
		messageBytes, err = hexStringToBytes(ExtractHexPrefix(lines[selected], 8))
		if err != nil || len(messageBytes) < 3 {
			return 0, false
		}
		msg, err = parsePLDMMessage(messageBytes)
		if err != nil || !msg.IsRequest || msg.PLDMType != 0x02 {
			return 0, false
		}
	}

	switch msg.CommandCode {
	case 0x21: // GetStateSensorReadings: SensorID is payload bytes 0:1.
		if len(messageBytes) < 5 {
			return 0, false
		}
		return uint16(messageBytes[3]) | uint16(messageBytes[4])<<8, true
	case 0x0A: // PlatformEventMessage: only EventClass SensorEvent has a SensorID.
		if len(messageBytes) < 8 || messageBytes[5] != 0x00 {
			return 0, false
		}
		return uint16(messageBytes[6]) | uint16(messageBytes[7])<<8, true
	default:
		return 0, false
	}
}

// GetReadFileByTypeFileType reads FileType from an OEM ReadFileByType request.
// Responses do not carry that field, so their type comes from the correlated
// request when it is present in the loaded journal.
func IsReadFileByTypeResponse(line string) bool {
	messageBytes, err := hexStringToBytes(ExtractHexPrefix(line, 3))
	if err != nil {
		return false
	}
	msg, err := parsePLDMMessage(messageBytes)
	return err == nil && !msg.IsRequest && msg.PLDMType == 0x3F && msg.CommandCode == 0x0B
}

func GetReadFileByTypeFileType(lines []string, selected int) (uint16, bool) {
	if selected < 0 || selected >= len(lines) {
		return 0, false
	}
	messageBytes, err := hexStringToBytes(ExtractHexPrefix(lines[selected], 5))
	if err != nil || len(messageBytes) < 3 {
		return 0, false
	}
	msg, err := parsePLDMMessage(messageBytes)
	if err != nil || msg.PLDMType != 0x3F || msg.CommandCode != 0x0B {
		return 0, false
	}
	if !msg.IsRequest {
		selected = FindCorrelatedLine(lines, selected)
		if selected < 0 {
			return 0, false
		}
		messageBytes, err = hexStringToBytes(ExtractHexPrefix(lines[selected], 5))
		if err != nil || len(messageBytes) < 5 {
			return 0, false
		}
		msg, err = parsePLDMMessage(messageBytes)
		if err != nil || !msg.IsRequest || msg.PLDMType != 0x3F || msg.CommandCode != 0x0B {
			return 0, false
		}
	}
	if len(messageBytes) < 5 {
		return 0, false
	}
	return uint16(messageBytes[3]) | uint16(messageBytes[4])<<8, true
}

// GetStateSensorReadingsSensorID is retained for callers that only need the
// older command-specific API.
func GetStateSensorReadingsSensorID(lines []string, selected int) (uint16, bool) {
	if selected < 0 || selected >= len(lines) {
		return 0, false
	}
	messageBytes, err := hexStringToBytes(ExtractHexPrefix(lines[selected], 3))
	if err != nil || len(messageBytes) < 3 {
		return 0, false
	}
	msg, err := parsePLDMMessage(messageBytes)
	if err != nil || msg.PLDMType != 0x02 || msg.CommandCode != 0x21 {
		return 0, false
	}
	return GetStateSensorPDRSensorID(lines, selected)
}

// annotateEnum returns "rawValue (name)" when the name is known, otherwise
// the raw value string as-is.
func annotateEnum(raw uint64, table map[uint64]string) string {
	name, ok := table[raw]
	if !ok {
		return fmt.Sprintf("0x%02X (%d)", raw, raw)
	}
	return fmt.Sprintf("0x%02X — %s", raw, name)
}

// Enum annotation tables — defined here so YAML comments stay the
// single authoritative source; these are only used for display.
var eventClassNames = map[uint64]string{
	0x00: "SensorEvent",
	0x01: "EffecterEvent",
	0x02: "RedfishTaskExecutedEvent",
	0x03: "RedfishMessageEvent",
	0x04: "PLDMPDRRepositoryChgEvent",
	0x05: "PLDMMessagePollEvent",
	0x06: "HeartbeatTimerElapsedEvent",
	0x07: "CPEREvent",
	0x08: "StructuredAttributeEvent",
}

var sensorEventClassNames = map[uint64]string{
	0x00: "SensorOpState",
	0x01: "StateSensorState",
	0x02: "NumericSensorState",
}

var sensorOpStateNames = map[uint64]string{
	0x00: "Enabled",
	0x01: "Disabled",
	0x02: "Unavailable",
	0x03: "StatusUnknown",
	0x04: "Failed",
	0x05: "Initializing",
	0x06: "ShuttingDown",
	0x07: "InTest",
}

var sensorDataSizeNames = map[uint64]string{
	0x00: "uint8",
	0x01: "sint8",
	0x02: "uint16",
	0x03: "sint16",
	0x04: "uint32",
	0x05: "sint32",
	0x06: "uint64",
	0x07: "sint64",
}

// ExtractHexBytes returns PLDM bytes, excluding the recognized receive envelope.
func ExtractHexBytes(line string) string {
	tokens := extractLogHexTokens(line, 0)
	return strings.Join(tokens[pldmLogOffset(line, tokens):], " ")
}

// ExtractHexPrefix extracts only the requested PLDM bytes plus a bounded
// lookahead to recognize the optional receive envelope.
func ExtractHexPrefix(line string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	tokens := extractLogHexTokens(line, max(5, maxBytes+2))
	tokens = tokens[pldmLogOffset(line, tokens):]
	return strings.Join(tokens[:min(maxBytes, len(tokens))], " ")
}

// pldmLogOffset recognizes the observed pldmd receive format:
// Rx: 08 01 <PLDM header> <payload>. Both requests and responses can arrive
// in this envelope. Bare host messages and transmitted messages retain their
// original header. Require a complete, plausible header before stripping it.
func pldmLogOffset(line string, tokens []string) int {
	rx, tx := strings.Index(line, "Rx:"), strings.Index(line, "Tx:")
	if rx < 0 || (tx >= 0 && tx < rx) || len(tokens) < 5 || tokens[0] != "08" || tokens[1] != "01" {
		return 0
	}
	header, err := parseHexByte(tokens[2])
	if err != nil || (header&0xE0 != 0x80 && header&0xE0 != 0x00) {
		return 0
	}
	typeByte, err := parseHexByte(tokens[3])
	if err != nil || typeByte&0xC0 != 0 {
		return 0
	}
	switch typeByte {
	case 0x00, 0x02, 0x03, 0x04, 0x05, 0x3F:
		return 2
	}
	return 0
}

// A zero limit reads the whole message; bounded callers avoid scanning large
// payloads. Strip ANSI per token so filtering highlights do not change parsing.
func extractLogHexTokens(line string, limit int) []string {
	rxPos := strings.Index(line, "Rx:")
	txPos := strings.Index(line, "Tx:")
	startPos := -1
	if rxPos >= 0 && (txPos < 0 || rxPos < txPos) {
		startPos = rxPos + 3
	} else if txPos >= 0 {
		startPos = txPos + 3
	}
	if startPos < 0 {
		return nil
	}
	result := make([]string, 0, 16)
	remainder := line[startPos:]
	for len(remainder) > 0 {
		remainder = strings.TrimLeft(remainder, " \t\r\n")
		if len(remainder) == 0 {
			break
		}
		end := strings.IndexAny(remainder, " \t\r\n")
		if end < 0 {
			end = len(remainder)
		}
		field := ansiStripRe.ReplaceAllString(remainder[:end], "")
		remainder = remainder[end:]
		if field == "" {
			continue
		}
		if _, err := parseHexByte(field); err != nil {
			break
		}
		result = append(result, field)
		if limit > 0 && len(result) == limit {
			break
		}
	}
	return result
}

// MatchesSemanticPatterns checks whether a hex payload string (space-separated
// bytes from ExtractHexBytes) matches any of the patterns returned by
// ResolveSemanticFilter. Patterns are matched against exact byte positions in
// the PLDM header and payload:
//   - "TT"        → token[1] == TT                              (type-only)
//   - "TT CC"     → token[1] == TT  AND token[2] == CC          (type + command)
//   - "TT CC FFFF"→ type+command match AND FileType uint16 LE
//     at tokens[3:4] == FFFF                       (OEM file type)
//   - "TT CC HHHHHHHH" → request with FileNameHandle uint32 LE at tokens[3:7]
//
// The FileType field is the first payload field (bytes 3-4 of the raw message)
// for all OEM commands that carry it, encoded little-endian.
func MatchesSemanticPatterns(rawHex string, patterns []string) bool {
	tokens := strings.Fields(strings.ToUpper(rawHex))
	if len(tokens) < 3 {
		return false
	}
	typeToken := tokens[1]
	cmdToken := tokens[2]

	for _, pat := range patterns {
		parts := strings.Fields(strings.ToUpper(pat))
		switch len(parts) {
		case 1:
			if typeToken == parts[0] {
				return true
			}
		case 2:
			if typeToken == parts[0] && cmdToken == parts[1] {
				return true
			}
		case 3:
			// Filename handles use eight hex digits and are present only in requests.
			if len(parts[2]) == 8 {
				if len(tokens) < 7 || typeToken != parts[0] || cmdToken != parts[1] {
					continue
				}
				header, err := strconv.ParseUint(tokens[0], 16, 8)
				if err != nil || header&0x80 == 0 {
					continue
				}
				var handle uint64
				valid := true
				for i := 0; i < 4; i++ {
					b, err := strconv.ParseUint(tokens[3+i], 16, 8)
					if err != nil {
						valid = false
						break
					}
					handle |= b << (8 * i)
				}
				if valid && fmt.Sprintf("%08X", handle) == parts[2] {
					return true
				}
				continue
			}
			// OEM file-type match: "3F CC FFFF"
			// FileType is a uint16 LE at payload offset 0 (raw tokens 3 and 4).
			if typeToken != parts[0] || cmdToken != parts[1] {
				continue
			}
			if len(tokens) < 5 {
				continue
			}
			// Reconstruct the uint16 from two little-endian bytes.
			var lo, hi uint8
			n1, _ := fmt.Sscanf(tokens[3], "%02X", &lo)
			n2, _ := fmt.Sscanf(tokens[4], "%02X", &hi)
			if n1 != 1 || n2 != 1 {
				continue
			}
			ftHex := fmt.Sprintf("%04X", uint16(lo)|uint16(hi)<<8)
			if ftHex == parts[2] {
				return true
			}
		}
	}
	return false
}

// resolveCache caches the last resolution so repeated calls with the same term
// (every log line during a single filter pass) are O(1) map lookups.
var resolveCache = struct {
	term   string
	result []string
}{}

// ResolveSemanticFilter takes a user search term and returns a slice of "TT CC"
// hex strings (e.g. ["02 51"]) that should be matched against a log line's raw
// hex payload using MatchesSemanticPatterns. It checks (case-insensitively):
//   - Command names            (e.g. "getpdr"          → ["02 51"])
//   - Substrings of names      (e.g. "pdr"             → ["02 50","02 51","02 52","02 53"])
//   - PLDM type names          (e.g. "platform"        → ["02"])
//   - "TypeName CommandName"   (e.g. "platform getpdr" → ["02 51"])
//
// The index is built once at spec-load time; results are cached per term so
// the function is effectively O(1) for repeated calls with the same term.
// Returns nil when no semantic match is found (caller falls back to raw string match).
func ResolveSemanticFilter(term string) []string {
	if pldmSpec == nil {
		if err := loadPLDMSpec(); err != nil {
			return nil
		}
	}

	termLower := strings.ToLower(strings.TrimSpace(term))
	if termLower == "" {
		return nil
	}

	// Cache hit — same term as last call (common during a filter pass over many lines)
	if resolveCache.term == termLower {
		return resolveCache.result
	}

	result := resolveSemanticFilterUncached(termLower)

	resolveCache.term = termLower
	resolveCache.result = result
	return result
}

// resolveSemanticFilterUncached does the actual index lookup. Called at most
// once per unique term thanks to the cache in ResolveSemanticFilter.
func resolveSemanticFilterUncached(termLower string) []string {
	if patterns := resolveFileNameFilter(termLower); len(patterns) > 0 {
		return patterns
	}
	collect := func(entries []semanticIndexEntry) []string {
		out := make([]string, len(entries))
		for i, e := range entries {
			out[i] = e.typeHex + " " + e.cmdHex
		}
		return out
	}

	// 1. Exact command name
	var matched []semanticIndexEntry
	for _, e := range semanticIndex {
		if e.nameLower == termLower {
			matched = append(matched, e)
		}
	}
	if len(matched) > 0 {
		return collect(matched)
	}

	// 2. Substring of command name
	for _, e := range semanticIndex {
		if strings.Contains(e.nameLower, termLower) {
			matched = append(matched, e)
		}
	}
	if len(matched) > 0 {
		return collect(matched)
	}

	// 3. Type-name only
	for tNameLower, tHex := range semanticByType {
		if termLower == tNameLower || strings.Contains(tNameLower, termLower) {
			return []string{tHex}
		}
	}

	// 4. OEM file type name (exact or substring) — e.g. "pel", "dump", "bmc_dump".
	// Emits "3F CC FFFF" patterns for every OEM command that carries a FileType field,
	// covering all commands that have "FileType:2" as their first request/response field.
	var ftPatterns []string
	for _, ft := range fileTypeIndex {
		if ft.nameLower == termLower || strings.Contains(ft.nameLower, termLower) {
			// Emit a pattern for every OEM command that carries FileType.
			for cmdCode, cmd := range pldmSpec.OEM {
				hasFileType := false
				for _, f := range cmd.Request {
					if strings.HasPrefix(f, "FileType:") {
						hasFileType = true
						break
					}
				}
				if !hasFileType {
					for _, f := range cmd.Response {
						if strings.HasPrefix(f, "FileType:") {
							hasFileType = true
							break
						}
					}
				}
				if hasFileType {
					cd := strings.TrimPrefix(strings.TrimPrefix(cmdCode, "0x"), "0X")
					if len(cd) == 1 {
						cd = "0" + cd
					}
					ftPatterns = append(ftPatterns, "3F "+strings.ToUpper(cd)+" "+ft.fileTypeHex)
				}
			}
		}
	}
	if len(ftPatterns) > 0 {
		return ftPatterns
	}

	// 5. Two-word "TypeName CommandName"
	parts := strings.Fields(termLower)
	if len(parts) == 2 {
		tHex, tOk := semanticByType[parts[0]]
		if !tOk {
			for tNameLower, h := range semanticByType {
				if strings.Contains(tNameLower, parts[0]) {
					tHex = h
					tOk = true
					break
				}
			}
		}
		if tOk {
			for _, e := range semanticIndex {
				if e.typeHex == tHex && strings.Contains(e.nameLower, parts[1]) {
					matched = append(matched, e)
				}
			}
			if len(matched) > 0 {
				return collect(matched)
			}
		}
	}

	return nil
}

var (
	ansiStripRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	isoRe       = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?([+-]\d{2}:\d{2}|Z)`)
	syslogRe    = regexp.MustCompile(`[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}(\.\d+)?`)
)

// extractTimestamp parses a timestamp from the start of a log line.
// Handles syslog ("Jun  2 23:10:18") and ISO 8601 ("2024-06-02T23:10:18Z").
// Returns zero time when no recognisable timestamp is found.
func extractTimestamp(line string) time.Time {
	clean := ansiStripRe.ReplaceAllString(line, "")

	if t, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", isoRe.FindString(clean)); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z07:00", isoRe.FindString(clean)); err == nil {
		return t
	}
	if m := syslogRe.FindString(clean); m != "" {
		year := time.Now().Year()
		for _, layout := range []string{"Jan  2 15:04:05.999999", "Jan  2 15:04:05", "Jan _2 15:04:05.999999", "Jan _2 15:04:05"} {
			if t, err := time.Parse(layout, m); err == nil {
				return t.AddDate(year, 0, 0)
			}
		}
	}
	return time.Time{}
}

// lineEntry is the pre-parsed form of one filteredLogLines entry.
// Valid is false when the line contains no parseable PLDM header.
type lineEntry struct {
	Valid       bool
	InstanceID  uint8
	IsRequest   bool
	PLDMType    uint8
	CommandCode uint8
	TS          time.Time
}

// lineIndex is rebuilt by BuildLineIndex whenever filteredLogLines changes.
// FindCorrelatedLine reads from it instead of re-parsing every line on each keypress.
// lineIndexMu guards lineIndex for concurrent access (background build + UI read).
var (
	lineIndex   []lineEntry
	lineIndexMu sync.RWMutex
)

// BuildLineIndex pre-parses every line in lines into lineIndex.
// Safe to call from a goroutine; FindCorrelatedLine holds the read lock.
func BuildLineIndex(lines []string) {
	// Header correlation does not need command definitions. Loading them here
	// allowed concurrent background index builds to mutate the semantic maps.
	idx := make([]lineEntry, len(lines))
	for i, line := range lines {
		idx[i] = parseLineEntry(line)
	}
	lineIndexMu.Lock()
	lineIndex = idx
	lineIndexMu.Unlock()
}

// parseLineEntry extracts only what FindCorrelatedLine needs from a single line.
func parseLineEntry(line string) lineEntry {
	// Use the same header boundary as display, semantic filters, and PDR lookup.
	tokens := strings.Fields(ExtractHexPrefix(line, 3))
	if len(tokens) < 3 {
		return lineEntry{}
	}
	b := make([]byte, 3)
	for i := 0; i < 3; i++ {
		v, err := parseHexByte(tokens[i])
		if err != nil {
			return lineEntry{}
		}
		b[i] = v
	}
	return lineEntry{
		Valid:       true,
		InstanceID:  b[0] & 0x1F,
		IsRequest:   (b[0] & 0x80) != 0,
		PLDMType:    b[1] & 0x3F,
		CommandCode: b[2],
		TS:          extractTimestamp(line),
	}
}

// parseHexByte parses a 2-char hex string into a byte without allocating.
func parseHexByte(s string) (byte, error) {
	if len(s) != 2 {
		return 0, fmt.Errorf("not a hex byte: %q", s)
	}
	hi, ok1 := hexNibble(s[0])
	lo, ok2 := hexNibble(s[1])
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("not a hex byte: %q", s)
	}
	return hi<<4 | lo, nil
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// FindCorrelatedLine scans lines for the PLDM message that correlates with the
// message at fromIdx. Correlation is defined as: same PLDMType + CommandCode +
// InstanceID, opposite IsRequest direction, appearing within a 5-second window.
//
// Uses lineIndex (built by BuildLineIndex) when available so no per-line
// string parsing is done at keypress time.
//
// Returns the index of the correlated line in lines, or -1 if not found.
func FindCorrelatedLine(lines []string, fromIdx int) int {
	if fromIdx < 0 || fromIdx >= len(lines) {
		return -1
	}

	lineIndexMu.RLock()
	idx := lineIndex
	lineIndexMu.RUnlock()

	// Rebuild synchronously if the background goroutine hasn't finished yet.
	if len(idx) != len(lines) {
		BuildLineIndex(lines)
		lineIndexMu.RLock()
		idx = lineIndex
		lineIndexMu.RUnlock()
	}

	src := idx[fromIdx]
	if !src.Valid {
		return -1
	}

	// Search forward first (request → response), then backward.
	directions := []struct{ start, end, step int }{
		{fromIdx + 1, len(lines), 1},
		{fromIdx - 1, -1, -1},
	}

	for _, d := range directions {
		for i := d.start; i != d.end; i += d.step {
			cand := idx[i]
			if !cand.Valid {
				continue
			}
			if cand.PLDMType != src.PLDMType ||
				cand.CommandCode != src.CommandCode ||
				cand.InstanceID != src.InstanceID ||
				cand.IsRequest == src.IsRequest {
				continue
			}
			// Timestamp window check (skip if either timestamp is unavailable).
			if !src.TS.IsZero() && !cand.TS.IsZero() {
				diff := cand.TS.Sub(src.TS)
				if diff < 0 {
					diff = -diff
				}
				if diff > 5*time.Second {
					continue
				}
			}
			return i
		}
	}
	return -1
}

// Made with Bob
