# LazyDebugger

A modified version of [lazydebugger](https://github.com/Lifailon/lazydebugger) with an additional **pldm_verbose** filter mode for debugging PLDM (Platform Level Data Model) daemon logs.

## What's New

### PLDM Verbose Filter Mode

Added a new filter mode specifically for pldmd daemon debugging:
- **Filters**: Shows only lines from pldmd containing "Rx:" (receive) or "Tx:" (transmit) messages
- **Highlights**: 
  - Rx: messages in green
  - Tx: messages in yellow
  - Additional filter text in blue (if provided)
- **Use Case**: Reduces noise when debugging PLDM communication by focusing only on message exchanges

## Filter Modes

LazyDebugger now supports 5 filter modes (cycle through with Up/Down or PgUp/PgDown):

1. **Default** - Exact search with case sensitivity
2. **Fuzzy** - Fuzzy search without case sensitivity
3. **Regex** - Regular expression search
4. **PLDM Verbose** - ⭐ NEW: Filter pldmd Rx:/Tx: messages
5. **Timestamp** - Filter by date range

## Installation

```bash
cd /home/manojeda/Documents/experiments/lazydebugger
go build -o lazydebugger
sudo cp lazydebugger /usr/local/bin/
```

## Usage

### Basic Usage

Run lazydebugger like lazydebugger:
```bash
./lazydebugger
```

### Using PLDM Verbose Filter

1. Start lazydebugger
2. Navigate to the filter window (press `/` or Tab)
3. Press `Up` or `PgUp` repeatedly to cycle through filter modes until you see **"Filter (PLDM Verbose)"**
4. (Optional) Type additional filter text to narrow down results
5. View filtered pldmd Rx:/Tx: messages with color highlighting

### Example Workflow

```bash
# Start lazydebugger
./lazydebugger

# In the TUI:
# 1. Press Tab to go to filter window
# 2. Press Up/PgUp until "Filter (PLDM Verbose)" appears
# 3. Type any additional filter text (optional)
# 4. Press Tab to view filtered logs
```

### With SSH Connection

```bash
./lazydebugger --ssh "user@remote-host"
# Then switch to PLDM Verbose filter mode in the TUI
```

### With Custom Path (for log files)

```bash
./lazydebugger --custom-path /home/manojeda/Documents/debug-logs
# or use short form:
./lazydebugger -p /home/manojeda/Documents/debug-logs
# Then switch to PLDM Verbose filter mode in the TUI
```

**Note**: This must be run in an actual terminal (not through command execution), as lazydebugger is a TUI application.

## Technical Details

### Changes Made to lazydebugger

1. **Added pldmVerboseFilter function** (line ~6156):
   - Filters lines containing "pldmd" AND ("Rx:" OR "Tx:")
   - Applies additional text filtering if provided
   - Color highlights Rx: (green) and Tx: (yellow)

2. **Updated applyFilter switch statement** (line ~6008):
   - Added case "pldm_verbose" to handle the new filter mode

3. **Updated setFilterModeRight function** (line ~9490):
   - Added "Filter (PLDM Verbose)" in the filter mode cycle

4. **Updated setFilterModeLeft function** (line ~9605):
   - Added "Filter (PLDM Verbose)" in the reverse filter mode cycle

### Filter Logic

```go
// Checks if line contains pldmd AND (Rx: OR Tx:)
if strings.Contains(lineLower, "pldmd") && 
   (strings.Contains(inputLine, "Rx:") || strings.Contains(inputLine, "Tx:"))
```

## Project Structure

```
lazydebugger/
├── main.go              # Modified lazydebugger with pldm_verbose filter
├── main_test.go         # Original tests
├── go.mod
├── go.sum
├── lazydebugger         # Compiled binary
└── README.md            # This file
```

## Maintaining Updates from Upstream

Since this is a fork of lazydebugger, to update:

1. Check for new lazydebugger releases
2. Copy new source to project directory
3. Re-apply the pldm_verbose filter changes (search for "PLDM" comments in code)
4. Rebuild

## Adding More Custom Filters

To add additional custom filters, follow the same pattern:

1. Add a new filter function (like `pldmVerboseFilter`)
2. Add a case in the `applyFilter` switch statement
3. Add the filter mode in `setFilterModeRight` and `setFilterModeLeft`
4. Rebuild

## Example: Adding an SSH Filter

```go
// Add after pldmVerboseFilter function
func (app *App) sshAuthFilter(inputLine, filter string) string {
    lineLower := strings.ToLower(inputLine)
    if strings.Contains(lineLower, "sshd") && 
       (strings.Contains(inputLine, "Accepted") || strings.Contains(inputLine, "Failed")) {
        // Apply highlighting...
        return inputLine
    }
    return ""
}
```

## Dependencies

- Go 1.21 or higher
- All original lazydebugger dependencies (automatically managed by go.mod)

## Original Features

All original lazydebugger features are preserved:
- Multiple log sources (journald, files, Docker, Kubernetes, etc.)
- SSH remote log viewing
- Color modes (default, tailspin, bat)
- Timestamp filtering
- Priority and boot filtering
- And more...

See [lazydebugger documentation](https://github.com/Lifailon/lazydebugger) for complete feature list.

## License

This is a modified version of lazydebugger. Please refer to the original project for license terms.

## Contributing

This is an experimental project for debugging pldmd. Suggestions for improvements are welcome.
