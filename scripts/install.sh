#!/bin/bash
set -euo pipefail

repo="manojkiraneda/lazydebugger"
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
    linux|darwin|openbsd|freebsd) ;;
    *) echo "Unsupported operating system: $os" >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "Unsupported processor architecture" >&2; exit 1 ;;
esac

binDir="${LAZYDEBUGGER_BIN_DIR:-$HOME/.local/bin}"
configDir="${LAZYDEBUGGER_CONFIG_DIR:-$HOME/.config/lazydebugger}"
configPath="$configDir/config.yml"
version="${LAZYDEBUGGER_VERSION:-}"
if [ -z "$version" ]; then
    releaseURL=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
    version="${releaseURL##*/}"
fi
case "$version" in
    v[0-9]*) ;;
    *) echo "Unable to determine a release version: $version" >&2; exit 1 ;;
esac

staging=$(mktemp -d "${TMPDIR:-/tmp}/lazydebugger-install.XXXXXX")
trap 'rm -rf "$staging"' EXIT
release="https://github.com/$repo/releases/download/$version"
curl -fsSL "$release/lazydebugger-$version-$os-$arch" -o "$staging/lazydebugger"

if [ ! -e "$configPath" ] && [ ! -L "$configPath" ]; then
    # Older releases have no config asset; use their tagged sample instead.
    if ! curl -fsSL "$release/config.yml" -o "$staging/config.yml"; then
        curl -fsSL "https://raw.githubusercontent.com/$repo/$version/config.yml" -o "$staging/config.yml"
    fi
fi

mkdir -p "$binDir" "$configDir"
install -m 755 "$staging/lazydebugger" "$binDir/lazydebugger"
if [ -f "$staging/config.yml" ]; then
    # noclobber also preserves a config created while downloads were running.
    if (set -C; cat "$staging/config.yml" > "$configPath") 2>/dev/null; then
        echo "Installed sample config: $configPath"
    elif [ ! -e "$configPath" ] && [ ! -L "$configPath" ]; then
        echo "Unable to install config: $configPath" >&2
        exit 1
    else
        echo "Preserved existing config: $configPath"
    fi
else
    echo "Preserved existing config: $configPath"
fi
if [ "$os" = darwin ] && command -v xattr >/dev/null 2>&1; then
    xattr -d com.apple.quarantine "$binDir/lazydebugger" 2>/dev/null || true
fi
echo "Installed lazydebugger $version: $binDir/lazydebugger"
case ":$PATH:" in
    *":$binDir:"*) ;;
    *) printf 'Add this directory to your shell PATH: %s\n' "$binDir" ;;
esac
