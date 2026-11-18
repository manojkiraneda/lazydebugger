$ErrorActionPreference = "Stop"
$repo = "manojkiraneda/lazydebugger"
$processor = $env:PROCESSOR_ARCHITEW6432
if (!$processor) { $processor = $env:PROCESSOR_ARCHITECTURE }
switch ($processor.ToLower()) {
    "amd64" { $arch = "amd64" }
    "arm64" { $arch = "arm64" }
    default { throw "Unsupported processor architecture: $processor" }
}
$binDir = Join-Path $env:LOCALAPPDATA "lazydebugger"
$configDir = Join-Path $HOME ".config\lazydebugger"
$configPath = Join-Path $configDir "config.yml"
$version = $env:LAZYDEBUGGER_VERSION
if (!$version) {
    $version = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name
}
if (!$version -or $version -notmatch '^v[0-9]') { throw "Unable to determine a release version" }
$release = "https://github.com/$repo/releases/download/$version"
$staging = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
New-Item -Path $staging -ItemType Directory | Out-Null
try {
    $binary = Join-Path $staging "lazydebugger.exe"
    $sample = Join-Path $staging "config.yml"
    Invoke-WebRequest -UseBasicParsing -Uri "$release/lazydebugger-$version-windows-$arch.exe" -OutFile $binary
    if (!(Test-Path -LiteralPath $configPath)) {
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$release/config.yml" -OutFile $sample
        } catch {
            Invoke-WebRequest -UseBasicParsing -Uri "https://raw.githubusercontent.com/$repo/$version/config.yml" -OutFile $sample
        }
    }
    New-Item -Path $binDir, $configDir -ItemType Directory -Force | Out-Null
    Copy-Item -LiteralPath $binary -Destination (Join-Path $binDir "lazydebugger.exe") -Force
    if (Test-Path -LiteralPath $sample) {
        # Do not replace a config created during installation.
        if (!(Test-Path -LiteralPath $configPath)) { [IO.File]::Copy($sample, $configPath, $false) }
    }
    $beforePath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($beforePath -split ';') -notcontains $binDir) {
        [Environment]::SetEnvironmentVariable("Path", "$beforePath;$binDir", "User")
    }
    if (($env:Path -split ';') -notcontains $binDir) { $env:Path += ";$binDir" }
    Write-Host "Installed lazydebugger $version in $binDir"
    Write-Host "Config (existing settings preserved): $configPath"
} finally {
    Remove-Item -LiteralPath $staging -Recurse -Force
}
