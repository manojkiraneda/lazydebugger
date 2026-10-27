package pldm

import (
	"fmt"
	"path"
	"strings"
	"testing"
)

func TestEveryFileTableNameResolves(t *testing.T) {
	ResolveSemanticFilter("NVRAM")
	if len(pldmSpec.OEMFileTable) != 138 {
		t.Fatalf("file table has %d entries, want 138", len(pldmSpec.OEMFileTable))
	}
	for handle, file := range pldmSpec.OEMFileTable {
		for _, alias := range strings.Split(file.Path, ",") {
			for _, term := range []string{alias, path.Base(alias), strings.ToUpper(path.Base(alias))} {
				patterns := ResolveSemanticFilter(term)
				for _, command := range []byte{4, 5, 6, 7} {
					payload := fmt.Sprintf("80 3F %02X %02X %02X %02X %02X", command, handle&255, (handle>>8)&255, (handle>>16)&255, (handle>>24)&255)
					if !MatchesSemanticPatterns(payload, patterns) {
						t.Errorf("%q did not match handle %d command %d", term, handle, command)
					}
				}
			}
		}
	}
}
