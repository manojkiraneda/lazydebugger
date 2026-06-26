# Log Parsers

This directory contains specialized log parsers for different daemon types.

## Structure

Each parser should be in its own subdirectory with the following structure:

```
parsers/
├── README.md
└── <parser_name>/
    ├── parser.go      # Core parsing logic
    ├── api.go         # Public API functions
    ├── commands.yml   # Command/message definitions (if applicable)
    └── README.md      # Parser-specific documentation
```

## Available Parsers

### PLDM Parser (`pldm/`)

Parses Platform Level Data Model (PLDM) messages from pldmd daemon logs.

**Features:**
- Parses PLDM message headers (Instance ID, Type, Command Code)
- Decodes payload based on DMTF specifications
- Supports 70+ commands from DSP0240, DSP0248, DSP0257
- Data-driven from YAML configuration

**Usage:**
```go
import "github.com/Lifailon/lazydebugger/parsers/pldm"

// Extract hex bytes from log line
hexBytes := pldm.ExtractHexBytes(logLine)

// Parse and display in a gocui view
pldm.ParseAndDisplay(view, hexBytes, frameColor, titleColor)
```

**Specifications:**
- DSP0240: PLDM Base Specification
- DSP0248: PLDM for Platform Monitoring and Control
- DSP0257: PLDM for FRU Data

## Adding a New Parser

To add support for a new daemon's log format:

1. Create a new directory: `parsers/<daemon_name>/`
2. Implement the core parsing logic in `parser.go`
3. Create a public API in `api.go` with functions like:
   - `ExtractData(line string) string` - Extract relevant data from log line
   - `ParseAndDisplay(view *gocui.View, data string, ...) error` - Parse and display
4. Add any configuration files (YAML, JSON, etc.)
5. Document the parser in a README.md
6. Create wrapper methods in the main package (e.g., `<daemon>_methods.go`)
7. Add filter mode in main.go if needed

## Design Principles

- **Separation of Concerns**: Keep parsing logic separate from UI code
- **Data-Driven**: Use configuration files for command/message definitions
- **Extensible**: Easy to add new commands or message types
- **Reusable**: Public API that can be used from different parts of the application
- **Well-Documented**: Clear documentation for each parser

## Example: Adding a Systemd Journal Parser

```
parsers/
└── systemd/
    ├── parser.go       # Parse systemd journal fields
    ├── api.go          # Public functions
    ├── fields.yml      # Field definitions
    └── README.md       # Documentation
```

Then in main.go:
```go
import "github.com/Lifailon/lazydebugger/parsers/systemd"

// In filter mode handler
case "systemd_verbose":
    data := systemd.ExtractFields(line)
    systemd.ParseAndDisplay(view, data, ...)