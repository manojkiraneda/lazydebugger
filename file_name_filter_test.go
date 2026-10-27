package main

import (
	"fmt"
	"github.com/awesome-gocui/gocui"
	"github.com/manojkiraneda/lazydebugger/v2/parsers/pldm"
	"strings"
	"testing"
)

func TestFileNameInParserPanel(t *testing.T) {
	gui, err := gocui.NewGui(gocui.OutputSimulator, true)
	if err != nil {
		t.Fatal(err)
	}
	defer gui.Close()
	view, err := gui.SetView("docker", 0, 0, 100, 40, 0)
	if err != nil && err != gocui.ErrUnknownView {
		t.Fatal(err)
	}
	for _, command := range []byte{4, 5, 6, 7} {
		for _, tc := range []struct {
			handle byte
			name   string
		}{{0, "PHYP-NVRAM"}, {1, "PHYP-NVRAM-CKSUM"}, {2, "80d00048.lid"}} {
			payload := fmt.Sprintf("80 3F %02X %02X 00 00 00 00 00 00 00 01 00 00 00 00 00 00 00 00 00 00 00", command, tc.handle)
			if err := pldm.ParseAndDisplay(view, payload, 0, 0); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(view.Buffer(), "\n    FileName: "+tc.name+"\n") || strings.Contains(view.Buffer(), "/var/lib/") {
				t.Fatalf("command %02X handle %d missing leaf name:\n%s", command, tc.handle, view.Buffer())
			}
		}
	}
}

func TestVerboseFileNameFilter(t *testing.T) {
	app := &App{}
	for _, tc := range []struct {
		name, payload, filter string
		want                  bool
	}{
		{"read file", "80 3F 04 00 00 00 00", "NVRAM", true},
		{"write file", "80 3F 05 02 00 00 00", "80d00048.lid", true},
		{"read file wrong handle", "80 3F 04 01 00 00 00", "80d00048.lid", false},
		{"write file response", "00 3F 05 00 00 00 00", "NVRAM", false},
		{"nvram read", "80 3F 06 00 00 00 00", "NVRAM", true},
		{"mctp framed request", "08 01 80 3F 06 00 00 00 00", "NVRAM", true},
		{"nvram write", "80 3F 07 00 00 00 00", "phyp-nvram", true},
		{"checksum", "80 3F 06 01 00 00 00", "PHYP-NVRAM-CKSUM", true},
		{"checksum excludes nvram", "80 3F 06 00 00 00 00", "PHYP-NVRAM-CKSUM", false},
		{"lid", "80 3F 06 02 00 00 00", "80d00048.lid", true},
		{"alternate path", "80 3F 07 02 00 00 00", "/var/lib/phosphor-software-manager/hostfw/running/80d00048.lid", true},
		{"full width", "80 3F 06 00 01 00 00", "NVRAM", false},
		{"response has no handle", "00 3F 06 00 00 00 00", "NVRAM", false},
		{"by type excluded", "80 3F 08 00 00 00 00", "NVRAM", false},
		{"truncated", "80 3F 06 00 00", "NVRAM", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			direction := "Tx: "
			if tc.name == "mctp framed request" {
				direction = "Rx: "
			}
			got := app.pldmVerboseFilter("pldmd "+direction+tc.payload, tc.filter) != ""
			if got != tc.want {
				t.Fatalf("matched = %v, want %v", got, tc.want)
			}
		})
	}
}
