package pldm

import (
	"fmt"
	"path"
	"strings"
)

func resolveFileNameFilter(term string) []string {
	var patterns []string
	for handle, file := range pldmSpec.OEMFileTable {
		if strings.Contains(strings.ToLower(file.Path), term) {
			// ReadFile, WriteFile, ReadFileIntoMemory, and WriteFileFromMemory
			// all start their request payload with a uint32 LE FileNameHandle.
			for _, command := range []byte{0x04, 0x05, 0x06, 0x07} {
				patterns = append(patterns, fmt.Sprintf("3F %02X %08X", command, handle))
			}
		}
	}
	return patterns
}

// LookupFileName returns the leaf filename for a host file table handle.
func LookupFileName(handle uint32) (string, bool) {
	if pldmSpec == nil {
		if err := loadPLDMSpec(); err != nil {
			return "", false
		}
	}
	if uint64(handle) >= uint64(len(pldmSpec.OEMFileTable)) {
		return "", false
	}
	filePath := strings.Split(pldmSpec.OEMFileTable[handle].Path, ",")[0]
	return path.Base(filePath), true
}
