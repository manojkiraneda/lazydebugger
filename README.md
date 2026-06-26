# LazyDebugger

A modified version of [lazyjournal](https://github.com/Lifailon/lazyjournal) with an additional **pldm_verbose** filter mode for debugging PLDM (Platform Level Data Model) daemon logs.

## What's New

### PLDM Verbose Filter Mode

Added a new filter mode specifically for pldmd daemon debugging:
- **Filters**: Shows only lines from pldmd containing "Rx:" (receive) or "Tx:" (transmit) messages
- **Highlights**: 
  - Rx: messages in green
  - Tx: messages in yellow
  - Additional filter text in blue (if provided)

## Filter Modes

LazyDebugger now supports 5 filter modes (cycle through with Up/Down or PgUp/PgDown):

1. **Default** - Exact search with case sensitivity
2. **Fuzzy** - Fuzzy search without case sensitivity
3. **Regex** - Regular expression search
4. **PLDM Verbose** - ⭐ NEW: Filter for parsing pldmd Rx:/Tx: messages
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

## License

This is a modified version of lazyjournal. Please refer to the original project for license terms.

## Contributing

This is an experimental project for debugging pldmd. Suggestions for improvements are welcome.
