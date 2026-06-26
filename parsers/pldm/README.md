# PLDM Parser

Parser for Platform Level Data Model (PLDM) messages from pldmd daemon logs.

## Overview

This parser decodes PLDM messages according to DMTF specifications, providing human-readable output of message headers and payloads.

## Supported Specifications

- **DSP0240**: PLDM Base Specification (9 commands)
- **DSP0248**: PLDM for Platform Monitoring and Control (50+ commands)
- **DSP0257**: PLDM for FRU Data (9 commands)

## Features

- ✅ Parses 3-byte PLDM message headers
- ✅ Decodes Instance ID, Message Type, PLDM Type, Command Code
- ✅ Interprets payloads based on command definitions
- ✅ Shows byte positions for all fields
- ✅ Displays both hex and decimal values
- ✅ Handles partial data with error messages
- ✅ Data-driven from YAML configuration

## Files

- `parser.go` - Core PLDM parsing logic
- `api.go` - Public API functions
- `commands.yml` - PLDM command definitions from DMTF specs

## PLDM Message Structure

```
Byte 0: RqD(bit7) D(bit6) Reserved(bit5) InstanceID(bits4-0)
Byte 1: HeaderVersion(bits7-6) PLDMType(bits5-0)
Byte 2: Command Code
Byte 3+: Payload
```

## Usage

### From Main Application

```go
import "github.com/Lifailon/lazydebugger/parsers/pldm"

// Extract hex bytes from log line
hexBytes := pldm.ExtractHexBytes(logLine)

// Parse and display
pldm.ParseAndDisplay(dockerView, hexBytes, frameColor, titleColor)
```

### Example Input

```
Rx: 80 02 21 59 55 00 00
```

### Example Output

```
╔═══════════════════════════════════════════════╗
║           PLDM Message Header                 ║
╚═══════════════════════════════════════════════╝

  Message Type:      Request
  Instance ID:       0
  Datagram:          false
  Header Version:    0
  PLDM Type:         0x02 (Platform)
  Command Code:      0x21 (GetStateSensorReadings)

╔═══════════════════════════════════════════════╗
║              Payload Data                     ║
╚═══════════════════════════════════════════════╝

  [3:4] SensorID: 0x5559 (21849)
  [5] BitMask: 0x00 (0)
  [6] RearmEventState: 0x00 (0)
```

## Command Definitions

Commands are defined in `commands.yml` with the following structure:

```yaml
platform:
  0x21:
    name: "GetStateSensorReadings"
    request: ["SensorID:2", "BitMask:1", "RearmEventState:1"]
    response: ["CC:1", "CompositeSensorCount:1", "SensorReadings:*"]
```

### Field Format

- `FieldName:N` - Fixed size field (N bytes)
- `FieldName:*` - Variable length field (remaining bytes)

## Adding New Commands

1. Find the command in the DMTF specification
2. Add entry to `commands.yml` under the appropriate PLDM type
3. Define request and response fields with sizes
4. Rebuild the application

Example:

```yaml
platform:
  0x22:
    name: "NewCommand"
    request: ["Field1:2", "Field2:4"]
    response: ["CC:1", "Result:1", "Data:*"]
```

## Supported PLDM Types

| Type | Name | Commands |
|------|------|----------|
| 0x00 | Base | 9 |
| 0x02 | Platform | 50+ |
| 0x03 | BIOS | 2 |
| 0x04 | FRU | 9 |
| 0x05 | Firmware Update | 4 |
| 0x06 | Redfish | - |
| 0x07 | OEM | - |

## References

- [DMTF DSP0240](https://www.dmtf.org/dsp/DSP0240) - PLDM Base Specification
- [DMTF DSP0248](https://www.dmtf.org/dsp/DSP0248) - PLDM for Platform Monitoring and Control
- [DMTF DSP0257](https://www.dmtf.org/dsp/DSP0257) - PLDM for FRU Data

## Contributing

When adding new commands:
1. Reference the official DMTF specification
2. Use correct field names from the spec
3. Specify accurate field sizes
4. Test with real PLDM messages
5. Document any special handling required