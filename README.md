# LazyDebugger

A modified version of [lazyjournal](https://github.com/Lifailon/lazyjournal) with an additional **pldm_verbose** filter mode for debugging PLDM (Platform Level Data Model) daemon logs.

## What's New

### PLDM Verbose Filter Mode

Added a new filter mode specifically for pldmd daemon debugging:
- **Filters**: Shows only lines from pldmd containing "Rx:" (receive) or "Tx:" (transmit) messages
- **Filename search**: Search `NVRAM`, `PHYP-NVRAM-CKSUM`, or `80d00048.lid` to match ReadFile, WriteFile, ReadFileIntoMemory, and WriteFileFromMemory requests by FileNameHandle. Matching is case-insensitive and accepts partial names or paths. The `oem_file_table` in `parsers/pldm/commands.yml` includes all supplied NVRAM and LID names and assigns handles sequentially from `0x00` (PHYP-NVRAM); responses do not carry a filename handle.
- **Highlights**:
  - Rx: messages in green
  - Tx: messages in yellow
  - Additional filter text in blue (if provided)

## Filter Modes

LazyDebugger supports 5 filter modes (cycle through with Up/Down or PgUp/PgDown):

1. **Default** - Exact search with case sensitivity
2. **Fuzzy** - Fuzzy search without case sensitivity
3. **Regex** - Regular expression search
4. **PLDM Verbose** - ⭐ NEW: Filter for parsing pldmd Rx:/Tx: messages
5. **Timestamp** - Filter by date range

## Installation

### Pre-built binary (Linux / macOS / OpenBSD / FreeBSD)

Download the latest release binary for your platform from the
[Releases page](https://github.com/manojkiraneda/lazydebugger/releases/latest) and install it:

```bash
# Replace <version>, <os>, and <arch> with your values, e.g.: v0.8.6, linux, amd64
VERSION=<version>
OS=<os>       # linux | darwin | openbsd | freebsd
ARCH=<arch>   # amd64 | arm64

curl -L "https://github.com/manojkiraneda/lazydebugger/releases/download/${VERSION}/lazydebugger-${VERSION}-${OS}-${ARCH}" \
  -o lazydebugger
chmod +x lazydebugger
sudo mv lazydebugger /usr/local/bin/
```

Or use the one-liner that detects your platform automatically:

```bash
curl -fsSL https://raw.githubusercontent.com/manojkiraneda/lazydebugger/main/scripts/install.sh | bash
```

### Windows

```powershell
irm https://raw.githubusercontent.com/manojkiraneda/lazydebugger/main/scripts/install.ps1 | iex
```

### Go install

If you have Go installed, you can install directly from the module path:

```bash
go install github.com/manojkiraneda/lazydebugger/v2@latest
```

The binary is placed in `$GOPATH/bin` (usually `~/go/bin`). Make sure that directory is on your `PATH`:

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

### Build from source

Requires [Go 1.21+](https://go.dev/dl/).

```bash
git clone https://github.com/manojkiraneda/lazydebugger.git
cd lazydebugger
go build -o lazydebugger .
sudo mv lazydebugger /usr/local/bin/
```

Or with the Makefile (automatically downloads Go if missing):

```bash
make install
```

## Usage

### Basic Usage

```bash
lazydebugger
```

For password-based SSH login:

```bash
lazydebugger --ssh "service@BMC"
```

To skip public-key authentication and use a password or keyboard-interactive login:

```bash
lazydebugger --ssh "service@localhost -o PubkeyAuthentication=no"
```

Enter your SSH password when prompted before the interface opens. The password
is held in memory for subsequent commands and is never saved to disk or placed
in command arguments. Each command opens a separate SSH connection. To connect
to another host requiring a different password, restart with that host in `--ssh`.

## License

This is a modified version of lazyjournal. Please refer to the original project for license terms.

## Contributing

This is an experimental project for debugging pldmd. Suggestions for improvements are welcome.
