package pldm

import (
	"regexp"
	"strings"
)

// Flight recorder headers contain no daemon name and put the bytes on the
// following line(s). Retain the timestamp and direction in one selectable row.
var flightHeaderRe = regexp.MustCompile(`^\s*(\d{4}-\d{2}-\d{2}\s+UTC\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s*:\s*(Rx|Tx)\s*:\s*(.*)$`)

var spacedDirectionRe = regexp.MustCompile(`\b(Rx|Tx)[ \t]+:`)

func logDirection(line string) (start, end int, receive bool) {
	rx, tx := strings.Index(line, "Rx:"), strings.Index(line, "Tx:")
	if rx >= 0 && (tx < 0 || rx < tx) {
		return rx, rx + 3, true
	}
	if tx >= 0 {
		return tx, tx + 3, false
	}
	if loc := spacedDirectionRe.FindStringIndex(line); loc != nil {
		return loc[0], loc[1], strings.HasPrefix(line[loc[0]:], "Rx")
	}
	return -1, -1, false
}

// NormalizeFlightRecorderLines joins only hex-only continuations belonging to
// a recorder header. Unrelated text and existing journal records stay intact.
// Incomplete records are retained, so a later streaming snapshot can complete
// them without losing their timestamp or consuming the next record.
func NormalizeFlightRecorderLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	header := ""
	var chunks []string
	flush := func() {
		if header != "" {
			line := header
			if len(chunks) > 0 {
				line += " " + strings.Join(chunks, " ")
			}
			result = append(result, line)
		}
		header = ""
		chunks = nil
	}
	for _, line := range lines {
		clean := ansiStripRe.ReplaceAllString(line, "")
		if match := flightHeaderRe.FindStringSubmatch(clean); match != nil {
			flush()
			header = match[1] + " : " + match[2] + ":"
			if payload := strings.TrimSpace(match[3]); payload != "" {
				if isHexDumpLine(payload) {
					chunks = append(chunks, strings.Join(strings.Fields(payload), " "))
				} else {
					// Do not turn a malformed inline record into a valid message.
					header += " " + payload
					flush()
				}
			}
			continue
		}
		if header != "" {
			if strings.TrimSpace(clean) == "" {
				continue
			}
			if isHexDumpLine(clean) {
				chunks = append(chunks, strings.Join(strings.Fields(clean), " "))
				continue
			}
			flush()
		}
		result = append(result, line)
	}
	flush()
	return result
}

func isHexDumpLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if _, err := parseHexByte(field); err != nil {
			return false
		}
	}
	return true
}

// IsMessageLogLine accepts existing pldmd logs and complete recorder records.
func IsMessageLogLine(line string) bool {
	if strings.Contains(strings.ToLower(line), "pldmd") {
		return strings.Contains(line, "Rx:") || strings.Contains(line, "Tx:")
	}
	clean := ansiStripRe.ReplaceAllString(line, "")
	return flightHeaderRe.MatchString(clean) && len(strings.Fields(ExtractHexPrefix(clean, 3))) == 3
}
