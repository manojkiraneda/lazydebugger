package pldm

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed commands.yml
var embeddedCommandsYML []byte

// PLDM Command Specification structures
type PLDMSpec struct {
	Types           map[string]string      `yaml:"types"`
	CompletionCodes map[string]string      `yaml:"completion_codes"`
	Base            map[string]PLDMCommand `yaml:"base"`
	Platform        map[string]PLDMCommand `yaml:"platform"`
	BIOS            map[string]PLDMCommand `yaml:"bios"`
	FRU             map[string]PLDMCommand `yaml:"fru"`
	Firmware        map[string]PLDMCommand `yaml:"firmware"`
	OEM             map[string]PLDMCommand `yaml:"oem"`
	OEMFileTypes    map[string]string      `yaml:"oem_file_types"`
}

type PLDMCommand struct {
	Name     string   `yaml:"name"`
	Request  []string `yaml:"request"`
	Response []string `yaml:"response"`
}

// Global PLDM spec loaded from YAML
var pldmSpec *PLDMSpec

// semanticIndex is built once after pldmSpec is loaded and reused on every
// filter call so we never rebuild it per log-line or per keystroke.
type semanticIndexEntry struct {
	nameLower string
	typeHex   string // 2-digit uppercase, e.g. "02"
	cmdHex    string // 2-digit uppercase, e.g. "51"
}

// fileTypeIndexEntry maps a lower-case file type name to its 4-digit uppercase
// hex value as it appears in the wire payload (little-endian uint16, zero-padded).
type fileTypeIndexEntry struct {
	nameLower string
	fileTypeHex string // 4-digit uppercase, e.g. "0003" for DUMP
}

var (
	semanticIndex  []semanticIndexEntry
	semanticByType map[string]string // lower type name → typeHex
	fileTypeIndex  []fileTypeIndexEntry
	// fileTypeByHex maps 4-digit uppercase hex → display name, used at render time.
	fileTypeByHex  map[string]string
)

// Load PLDM specification from embedded YAML
func loadPLDMSpec() error {
	pldmSpec = &PLDMSpec{}
	if err := yaml.Unmarshal(embeddedCommandsYML, pldmSpec); err != nil {
		return fmt.Errorf("failed to parse embedded commands.yml: %w", err)
	}

	// Build the semantic index once so ResolveSemanticFilter is O(1) per call.
	buildSemanticIndex()
	return nil
}

// buildSemanticIndex populates semanticIndex and semanticByType from pldmSpec.
// Called once by loadPLDMSpec.
func buildSemanticIndex() {
	typeGroups := []struct {
		typeCode string
		cmds     map[string]PLDMCommand
	}{
		{"0x00", pldmSpec.Base},
		{"0x02", pldmSpec.Platform},
		{"0x03", pldmSpec.BIOS},
		{"0x04", pldmSpec.FRU},
		{"0x05", pldmSpec.Firmware},
		{"0x3F", pldmSpec.OEM},
	}

	semanticByType = make(map[string]string, len(pldmSpec.Types))
	for hexStr, name := range pldmSpec.Types {
		d := strings.TrimPrefix(strings.TrimPrefix(hexStr, "0x"), "0X")
		if len(d) == 1 {
			d = "0" + d
		}
		semanticByType[strings.ToLower(name)] = strings.ToUpper(d)
	}

	semanticIndex = semanticIndex[:0]
	for _, tg := range typeGroups {
		td := strings.TrimPrefix(strings.TrimPrefix(tg.typeCode, "0x"), "0X")
		if len(td) == 1 {
			td = "0" + td
		}
		typeHex := strings.ToUpper(td)
		for cmdCode, cmd := range tg.cmds {
			cd := strings.TrimPrefix(strings.TrimPrefix(cmdCode, "0x"), "0X")
			if len(cd) == 1 {
				cd = "0" + cd
			}
			semanticIndex = append(semanticIndex, semanticIndexEntry{
				nameLower: strings.ToLower(cmd.Name),
				typeHex:   typeHex,
				cmdHex:    strings.ToUpper(cd),
			})
		}
	}

	// Build file type lookup tables from OEMFileTypes.
	fileTypeIndex = fileTypeIndex[:0]
	fileTypeByHex = make(map[string]string, len(pldmSpec.OEMFileTypes))
	for hexStr, name := range pldmSpec.OEMFileTypes {
		d := strings.TrimPrefix(strings.TrimPrefix(hexStr, "0x"), "0X")
		// Zero-pad to 4 digits (uint16 on the wire).
		for len(d) < 4 {
			d = "0" + d
		}
		h := strings.ToUpper(d)
		fileTypeByHex[h] = name
		fileTypeIndex = append(fileTypeIndex, fileTypeIndexEntry{
			nameLower:   strings.ToLower(name),
			fileTypeHex: h,
		})
	}
}

// LookupFileTypeName resolves a uint16 FileType value to its display name.
// Returns the name and true when found, empty string and false otherwise.
func LookupFileTypeName(fileTypeVal uint16) (string, bool) {
	h := fmt.Sprintf("%04X", fileTypeVal)
	name, ok := fileTypeByHex[h]
	return name, ok
}

// Parse PLDM message header
type PLDMMessage struct {
	InstanceID     uint8
	Reserved       bool
	Datagram       bool
	IsRequest      bool
	HeaderVersion  uint8
	PLDMType       uint8
	CommandCode    uint8
}

func parsePLDMMessage(bytes []byte) (*PLDMMessage, error) {
	if len(bytes) < 3 {
		return nil, fmt.Errorf("insufficient bytes for PLDM message (need at least 3, got %d)", len(bytes))
	}

	// Byte 0: RqD(bit7) D(bit6) Reserved(bit5) InstanceID(bits4-0)
	// Byte 1: HeaderVersion(bits7-6) PLDMType(bits5-0)
	// Byte 2: Command Code
	
	msg := &PLDMMessage{
		InstanceID:    bytes[0] & 0x1F,
		Reserved:      (bytes[0] & 0x20) != 0,
		Datagram:      (bytes[0] & 0x40) != 0,
		IsRequest:     (bytes[0] & 0x80) != 0,
		HeaderVersion: (bytes[1] >> 6) & 0x03,
		PLDMType:      bytes[1] & 0x3F,
		CommandCode:   bytes[2],
	}

	return msg, nil
}

// Get command definition based on type and command code
func getCommandDef(pldmType, cmdCode uint8) *PLDMCommand {
	if pldmSpec == nil {
		return nil
	}

	cmdKey := fmt.Sprintf("0x%02X", cmdCode)

	var cmdMap map[string]PLDMCommand
	switch pldmType {
	case 0x00:
		cmdMap = pldmSpec.Base
	case 0x02:
		cmdMap = pldmSpec.Platform
	case 0x03:
		cmdMap = pldmSpec.BIOS
	case 0x04:
		cmdMap = pldmSpec.FRU
	case 0x05:
		cmdMap = pldmSpec.Firmware
	case 0x3F:
		cmdMap = pldmSpec.OEM
	default:
		return nil
	}

	if cmd, ok := cmdMap[cmdKey]; ok {
		return &cmd
	}
	return nil
}

// Parse field definition (e.g., "FieldName:2" or "FieldName:*")
type FieldDef struct {
	Name string
	Size int // -1 for variable length (*)
}

func parseFieldDef(fieldStr string) FieldDef {
	parts := strings.Split(fieldStr, ":")
	if len(parts) != 2 {
		return FieldDef{Name: fieldStr, Size: 1}
	}

	size := 1
	if parts[1] == "*" {
		size = -1
	} else {
		if s, err := strconv.Atoi(parts[1]); err == nil {
			size = s
		}
	}

	return FieldDef{Name: parts[0], Size: size}
}

// ParseResult holds the result of parsing a field
type ParseResult struct {
	Field       string
	Value       string
	ByteRange   string
	Error       string
}

// Parse payload based on field definitions
func parsePayload(bytes []byte, fieldDefs []string) []ParseResult {
	var results []ParseResult
	offset := 0

	for _, fieldStr := range fieldDefs {
		field := parseFieldDef(fieldStr)
		result := ParseResult{Field: field.Name}
		
		if offset >= len(bytes) {
			result.Error = "insufficient bytes"
			result.ByteRange = fmt.Sprintf("[%d:?]", offset)
			results = append(results, result)
			continue
		}

		if field.Size == -1 {
			// Variable length - take remaining bytes
			remaining := bytes[offset:]
			result.ByteRange = fmt.Sprintf("[%d:%d]", offset, len(bytes)-1)
			result.Value = fmt.Sprintf("%s (%d bytes)", formatHexBytes(remaining), len(remaining))
			results = append(results, result)
			break
		} else {
			// Fixed length
			endOffset := offset + field.Size
			
			if endOffset > len(bytes) {
				// Not enough bytes - parse what we have
				availableBytes := len(bytes) - offset
				result.Error = fmt.Sprintf("need %d bytes, only %d available", field.Size, availableBytes)
				result.ByteRange = fmt.Sprintf("[%d:%d]", offset, len(bytes)-1)
				
				if availableBytes > 0 {
					partialBytes := bytes[offset:]
					// Format the partial data we have
					if availableBytes == 1 {
						result.Value = fmt.Sprintf("0x%02X (%d) [partial]", partialBytes[0], partialBytes[0])
					} else if availableBytes == 2 {
						val := uint16(partialBytes[0]) | uint16(partialBytes[1])<<8
						result.Value = fmt.Sprintf("0x%04X (%d) [partial]", val, val)
					} else if availableBytes == 3 {
						val := uint32(partialBytes[0]) | uint32(partialBytes[1])<<8 | uint32(partialBytes[2])<<16
						result.Value = fmt.Sprintf("0x%06X (%d) [partial]", val, val)
					} else {
						result.Value = fmt.Sprintf("%s [partial]", formatHexBytes(partialBytes))
					}
				} else {
					result.Value = "<no data>"
				}
				results = append(results, result)
				offset = len(bytes) // Move to end
				continue
			}
			
			fieldBytes := bytes[offset:endOffset]
			result.ByteRange = fmt.Sprintf("[%d:%d]", offset, endOffset-1)
			
			// Format based on size
			if field.Size == 1 {
				result.Value = fmt.Sprintf("0x%02X (%d)", fieldBytes[0], fieldBytes[0])
			} else if field.Size == 2 {
				val := uint16(fieldBytes[0]) | uint16(fieldBytes[1])<<8
				result.Value = fmt.Sprintf("0x%04X (%d)", val, val)
			} else if field.Size == 4 {
				val := uint32(fieldBytes[0]) |
					uint32(fieldBytes[1])<<8 |
					uint32(fieldBytes[2])<<16 |
					uint32(fieldBytes[3])<<24
				result.Value = fmt.Sprintf("0x%08X (%d)", val, val)
			} else {
				result.Value = formatHexBytes(fieldBytes)
			}
			
			results = append(results, result)
			offset = endOffset
		}
	}

	return results
}

func formatHexBytes(bytes []byte) string {
	var parts []string
	for _, b := range bytes {
		parts = append(parts, fmt.Sprintf("%02X", b))
	}
	return strings.Join(parts, " ")
}

// Parse hex string to bytes (internal function)
func hexStringToBytes(hexStr string) ([]byte, error) {
	hexParts := strings.Fields(hexStr)
	bytes := make([]byte, 0, len(hexParts))
	
	for _, part := range hexParts {
		// Remove any 0x prefix if present
		part = strings.TrimPrefix(part, "0x")
		part = strings.TrimPrefix(part, "0X")
		part = strings.TrimSpace(part)
		
		// Skip empty parts
		if part == "" {
			continue
		}
		
		// Each hex byte should be exactly 2 characters
		if len(part) != 2 {
			return nil, fmt.Errorf("invalid hex byte: '%s' (expected 2 hex digits)", part)
		}
		
		b, err := hex.DecodeString(part)
		if err != nil {
			return nil, fmt.Errorf("invalid hex: '%s' (%v)", part, err)
		}
		bytes = append(bytes, b...)
	}
	
	return bytes, nil
}

// All public API functions have been moved to api.go
// Internal helper functions remain here
