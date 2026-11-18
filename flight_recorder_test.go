package main

import (
	"strings"
	"testing"

	"github.com/manojkiraneda/lazydebugger/v2/parsers/pldm"
)

func TestFlightRecorderVerboseFiltering(t *testing.T) {
	input := []string{
		"2026-11-11 UTC 12:39:16.894775728 : Rx :", "85 02 0a 01 d0 06", "01 02",
		"2026-11-11 UTC 12:39:16.895645162 : Tx :", "05 02 0a 00 00",
		"unrelated text",
		"2026-11-11 UTC 12:37:20.004753020 : Rx :", "80 3f 06 00 00 00 00",
	}
	for _, mode := range []string{"snapshot", "apply filter"} {
		t.Run(mode, func(t *testing.T) {
			app := &App{testMode: true, selectFilterMode: "pldm_verbose", currentLogLines: input}
			getLines := func(filter string) []string {
				if mode == "snapshot" {
					return app.buildPldmFilteredSnapshot(input, filter)
				}
				app.filterText = filter
				app.applyFilter(false)
				return app.filteredLogLines
			}
			all := getLines("")
			if len(all) != 5 || pldm.ExtractHexBytes(all[0]) != "85 02 0a 01 d0 06 01 02" {
				t.Fatalf("messages not grouped: %q", all)
			}
			for _, filter := range []string{"PlatformEventMessage", "d0 06"} {
				got := getLines(filter)
				wantCount := 3
				if filter == "PlatformEventMessage" {
					wantCount = 4
				}
				if len(got) != wantCount || !strings.Contains(pldm.ExtractHexBytes(got[0]), "85 02") {
					t.Fatalf("filter %q: %q", filter, got)
				}
			}
		})
	}
}
