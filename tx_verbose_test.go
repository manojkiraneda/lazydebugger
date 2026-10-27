package main

import (
	"github.com/manojkiraneda/lazydebugger/v2/parsers/pldm"
	"strings"
	"testing"
)

func TestVerboseGetPDRResponseFromBMC(t *testing.T) {
	line := "Oct 26 02:47:40 p11bmc pldmd[1422]: Tx: 00 02 51 00 df 00 00 00 00 00 00 00 05 1e 00 de 00 00 00 01 0b 00 00 10 00 01 00 3d 00 04 60 00 00 01 00 00 00 00 00 01 04 01 02 80 06"
	app := &App{selectFilterMode: "pldm_verbose"}
	filtered := app.pldmVerboseFilter(line, "getpdr")
	if filtered == "" {
		t.Fatal("GetPDR Tx line disappeared from verbose filter")
	}
	extracted := pldm.ExtractHexBytes(filtered)
	if !strings.HasPrefix(extracted, "00 02 51 00 DF") && !strings.HasPrefix(strings.ToUpper(extracted), "00 02 51 00 DF") {
		t.Fatalf("Tx header changed: %q", extracted)
	}
	if !pldm.MatchesSemanticPatterns(pldm.ExtractHexPrefix(filtered, 5), []string{"02 51"}) {
		t.Fatal("GetPDR semantic search did not match Tx")
	}
	pldm.BuildLineIndex([]string{filtered})
}
