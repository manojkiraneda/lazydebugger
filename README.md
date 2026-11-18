# LazyDebugger

A modified version of [lazyjournal](https://github.com/Lifailon/lazyjournal) with an additional **pldm_verbose** filter mode for debugging PLDM (Platform Level Data Model) daemon logs.

## What's New

### PLDM Verbose Filter Mode

Added a new filter mode specifically for pldmd daemon debugging:
- **Filters**: Shows pldmd "Rx:" (receive) / "Tx:" (transmit) messages and PLDM flight recorder records such as `2026-11-11 UTC 12:39:16.894775728 : Rx :` followed by hex bytes. Wrapped payload lines are combined into one selectable message. Open the recorder file and select **PLDM Verbose** to filter by command name or bytes. The parser decodes the selected message automatically; Space jumps to its matching request or response within five seconds. UTC timestamps retain nanosecond precision for request/response correlation.
- **Filename search**: Search `NVRAM`, `PHYP-NVRAM-CKSUM`, or `80d00048.lid` to match ReadFile, WriteFile, ReadFileIntoMemory, and WriteFileFromMemory requests by FileNameHandle. Matching is case-insensitive and accepts partial names or paths. The `oem_file_table` in `parsers/pldm/commands.yml` includes all supplied NVRAM and LID names and assigns handles sequentially from `0x00` (PHYP-NVRAM); responses do not carry a filename handle.
- **Highlights**:
  - Rx: messages in green
  - Tx: messages in yellow
  - Additional filter text in blue (if provided)

In the **PLDM Message Parser** panel, drag with the left mouse button to select
lines, then press `y` to copy. For keyboard selection, focus the panel with Tab,
scroll to the first line, press `v`, and extend the selection with Up/Down or
`k`/`j`. Press `Y` to copy the entire decoded message; `y` without a selection
also copies all text. Esc clears the selection and resumes parser updates.
Selected text stays stable during background log refreshes. Copy uses the local
clipboard helper when available, otherwise OSC 52 (which requires clipboard
access enabled in your terminal). For character-level selection, most terminals
also support Shift-drag and their usual copy shortcut.
The parser shows the selected log's timestamp above the decoded message,
preserving its original precision and timezone. This timestamp is included when
copying the entire message; logs without a timestamp show `unavailable`.

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

## SSH analysis mode

```bash
lazydebugger --analysis-mode --ssh "service@BMC"
```

Analysis mode shows two panels: available plugins on the left and command output
on the right. Click a plugin to run it, or select it with Up/Down and press Enter to run its
commands on the BMC. Enter also reruns the selection while the output panel is focused. Tab switches panels; Up/Down and PgUp/PgDn scroll output. Esc cancels the
current run, and Ctrl+C exits. Press Enter again to rerun the selected plugin.

The bundled `vmi` plugin checks Redfish first, then queries D-Bus and BIOS in parallel:

1. **Redfish**: the hypervisor EthernetInterface resources for `eth0` and `eth1`.
2. **hypervisor-network-manager**: Address, Gateway, Origin (method), PrefixLength,
   and Enabled on each interface's IPv4/IPv6 `addr0` D-Bus object.
3. **BIOS**: all 16 VMI network attributes via `pldmtool`.

Redfish, D-Bus, and BIOS appear side by side in a compact comparison table,
with aligned property rows grouped by interface and IP version. Redfish
shows the first address in each IP family; omitted properties say `Not reported`.
Its interface-level `InterfaceEnabled` appears as `IF enabled`, separately from
D-Bus address `Enabled`. DHCPv4 enabled state, DHCPv6 operating mode, and IPv6
autoconfiguration are also displayed. The IPv4 subnet mask is converted to a prefix length. A failed check
is displayed while the remaining checks continue.

Redfish uses a session login: the application reuses the SSH username and password when available, creates a session, sends its `X-Auth-Token`
with both GET requests, and deletes the session after the Redfish checks. Secrets
travel over SSH stdin and are never printed or included in command arguments.
If SSH used a key or a different Redfish username is requested, only the missing
Redfish credentials are prompted. SSH key passphrases are never reused as account passwords.

```bash
./lazydebugger --analysis-mode --ssh "root@BMC" --redfish-user root
```

`--redfish-url` specifies the HTTPS base URL as reached **from the BMC** (default
`https://localhost`). Self-signed certificates are accepted by default. Use
`--redfish-insecure=false` to require certificate verification. The `redfish_paths` list in `analysis.yml` defines the two
EthernetInterface GET paths; `.netrc` is no longer used. Legacy VMI configurations
with `output: bios-values` are upgraded to run all three stages.

The **D-Bus** section displays property values without command text. Use **1**, **2**,
and **3** to jump to Redfish, D-Bus, and BIOS respectively.
Left/Right scroll horizontally in narrow terminals; Up/Down and PgUp/PgDn
scroll vertically. Scrolling stays in place while the live report refreshes.

Provide your own configuration using:

```bash
lazydebugger --analysis-mode --ssh "service@BMC" --analysis-config ./analysis.yml
```

```yaml
plugins:
  - name: vmi
    description: BMC information
    commands:
      - hostname
      - uname -a
      - uptime
```

Commands run sequentially through the BMC shell, supporting pipes and quoting.
Each command has a two-minute timeout; a failed command stops a generic command run and displays
the error. The VMI plugin continues to collect the remaining attributes. Output includes both stdout and stderr. SSH authentication uses the
same startup password prompt and connection options as normal SSH mode.
