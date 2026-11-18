package pldm

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const flightSample = `2026-11-11 UTC 12:39:16.894775728 : Rx :
85 02 0a 01 d0 06 01 02
2026-11-11 UTC 12:39:16.895645162 : Tx :
05 02 0a 00 00
2026-11-11 UTC 12:41:16.896614375 : Rx :
86 02 0a 01 d0 06 01 03
2026-11-11 UTC 12:41:16.897506313 : Tx :
06 02 0a 00 00
2026-11-11 UTC 12:43:16.898474510 : Rx :
87 02 0a 01 d0 06 01 04
2026-11-11 UTC 12:43:16.899327183 : Tx :
07 02 0a 00 00
2026-11-11 UTC 12:37:20.004753020 : Rx :
80 3f 06 00 00 00 00 00 00 f8 0f 00 00 08 00 00 40 01 00 00 00 00 00
2026-11-11 UTC 12:37:20.009852424 : Tx :
00 3f 06 00 00 00 08 00
2026-11-11 UTC 12:37:20.010163997 : Rx :
81 3f 04 01 00 00 00 00 00 00 00 10 00 00 00
2026-11-11 UTC 12:37:20.010342623 : Tx :
01 3f 04 00 10 00 00 00 40 00 00 00 00 00 00 00 00 00 00 00 99 68 44 c8`

func TestFlightRecorderSample(t *testing.T) {
	physical := strings.Split(flightSample, "\n")
	lines := NormalizeFlightRecorderLines(physical)
	if len(lines) != 10 {
		t.Fatalf("got %d messages, want 10", len(lines))
	}
	BuildLineIndex(lines)
	for i, line := range lines {
		if !IsMessageLogLine(line) {
			t.Fatalf("record %d was rejected: %s", i, line)
		}
		if got := ExtractHexBytes(line); got != physical[i*2+1] {
			t.Fatalf("record %d bytes = %q, want %q", i, got, physical[i*2+1])
		}
		message, err := hexStringToBytes(ExtractHexBytes(line))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := parsePLDMMessage(message)
		if err != nil || decoded.IsRequest != (i%2 == 0) {
			t.Fatalf("record %d decoded incorrectly: %+v, %v", i, decoded, err)
		}
		if got := FindCorrelatedLine(lines, i); got != i^1 {
			t.Fatalf("record %d counterpart = %d, want %d", i, got, i^1)
		}
	}
	want := time.Date(2026, 11, 11, 12, 39, 16, 894775728, time.UTC)
	if got := extractTimestamp(lines[0]); !got.Equal(want) {
		t.Fatalf("timestamp = %v, want %v", got, want)
	}
	if got := extractTimestamp(lines[1]).Sub(extractTimestamp(lines[0])); got != 869434*time.Nanosecond {
		t.Fatalf("response duration = %v", got)
	}
	if got := ExtractHexBytes(strings.Join(physical[:2], "\n")); got != physical[1] {
		t.Fatalf("raw multiline extraction = %q", got)
	}
}

func TestFlightRecorderBoundaries(t *testing.T) {
	header := "2026-11-11 UTC 12:39:16.894775728 : Rx :"
	for _, tc := range []struct {
		name        string
		input, want []string
	}{
		{"wrapped CRLF with blanks", []string{header + "\r", "85 02\r", "", "\t0A 01\r"}, []string{strings.Replace(header, "Rx :", "Rx:", 1) + " 85 02 0A 01"}},
		{"inline", []string{header + " 85 02 0a"}, []string{strings.Replace(header, "Rx :", "Rx:", 1) + " 85 02 0a"}},
		{"incomplete then complete", []string{header, header, "85 02 0a"}, []string{strings.Replace(header, "Rx :", "Rx:", 1), strings.Replace(header, "Rx :", "Rx:", 1) + " 85 02 0a"}},
		{"noise stops continuation", []string{header, "85 02 0a", "unrelated message", "de ad be ef"}, []string{strings.Replace(header, "Rx :", "Rx:", 1) + " 85 02 0a", "unrelated message", "de ad be ef"}},
		{"existing logs unchanged", []string{"pldmd Rx: 80 02 51", "pldmd Tx: 00 02 51 00"}, []string{"pldmd Rx: 80 02 51", "pldmd Tx: 00 02 51 00"}},
		{"malformed payload", []string{header, "85 02 invalid", "80 02 51"}, []string{strings.Replace(header, "Rx :", "Rx:", 1), "85 02 invalid", "80 02 51"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeFlightRecorderLines(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if again := NormalizeFlightRecorderLines(got); !reflect.DeepEqual(again, got) {
				t.Fatalf("normalizing twice changed records: %q", again)
			}
		})
	}
}

func TestFlightRecorderCorrelationRespectsTimestampWindow(t *testing.T) {
	lines := NormalizeFlightRecorderLines([]string{
		"2026-11-11 UTC 12:39:16 : Rx :", "85 02 0a 01 d0 06 01 02",
		"2026-11-11 UTC 12:41:16 : Tx :", "05 02 0a 00 00",
	})
	BuildLineIndex(lines)
	if got := FindCorrelatedLine(lines, 0); got != -1 {
		t.Fatalf("correlated an old reused instance ID: %d", got)
	}
}
