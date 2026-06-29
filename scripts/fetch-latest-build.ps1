param(
  [string]$Repo = "",
  [switch]$Stable,
  [switch]$DownloadOnly,
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]]$AppArgs
)

$ErrorActionPreference = "Stop"

function Fail {
  param([string]$Message)
  Write-Error $Message
  exit 1
}

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
  Fail "gh CLI not found. Install: https://cli.github.com/"
}

try {
  gh auth status | Out-Null
} catch {
  Fail "gh CLI not logged in. Run: gh auth login"
}

if ([string]::IsNullOrWhiteSpace($Repo)) {
  try {
    $Repo = (gh repo view --json nameWithOwner --jq .nameWithOwner 2>$null).Trim()
  } catch {
    $Repo = ""
  }
}

if ([string]::IsNullOrWhiteSpace($Repo)) {
  try {
    $remote = (git config --get remote.origin.url 2>$null).Trim()
  } catch {
    $remote = ""
  }

  if ($remote -match '^git@github\.com:(.+?)(?:\.git)?$') {
    $Repo = $Matches[1]
  } elseif ($remote -match '^https?://github\.com/(.+?)(?:\.git)?$') {
    $Repo = $Matches[1]
  } elseif ($remote -match '^ssh://git@github\.com/(.+?)(?:\.git)?$') {
    $Repo = $Matches[1]
  }
}

if ([string]::IsNullOrWhiteSpace($Repo)) {
  Fail "unable to detect repo, pass -Repo owner/name"
}

$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()

if ($IsWindows) {
  switch ($arch) {
    "x64" { $target = "windows-amd64" }
    "arm64" { $target = "windows-arm64" }
    default { Fail "unsupported Windows architecture: $arch" }
  }
} elseif ($IsMacOS) {
  switch ($arch) {
    "x64" { $target = "darwin-amd64" }
    "arm64" { $target = "darwin-arm64" }
    default { Fail "unsupported macOS architecture: $arch" }
  }
} elseif ($IsLinux) {
  switch ($arch) {
    "x64" { $target = "linux-amd64" }
    "arm64" { $target = "linux-arm64" }
    "arm" { $target = "linux-armv7" }
    "riscv64" { $target = "linux-riscv64" }
    default { Fail "unsupported Linux architecture: $arch" }
  }
} else {
  Fail "unsupported operating system"
}

if ($target.StartsWith("windows-")) {
  $ext = ".exe"
} else {
  $ext = ""
}

if ($Stable) {
  $mode = "stable"
} else {
  $mode = "dev"
}

Write-Host "repo:    $Repo"
Write-Host "branch:  master/main"
Write-Host "mode:    $mode"
Write-Host "target:  $target"

if ($Stable) {
  Write-Host "release select: latest stable release (no build wait)"
  $tagCandidates = gh api "/repos/$Repo/releases?per_page=100" --jq '.[] | select(.draft == false and .prerelease == false) | .tag_name'
  $tag = $tagCandidates | Select-Object -First 1
} else {
  Write-Host "release select: fixed dev prerelease tag"
  $tag = "dev"
}

if ([string]::IsNullOrWhiteSpace($tag)) {
  Fail "no matching release found for repo: $Repo"
}

$binName = "firescribe-$target$ext"
$shaName = "$binName.sha256"
$runDir = Join-Path (Get-Location).Path ("test/{0}/{1}" -f $target, $tag)
New-Item -ItemType Directory -Path $runDir -Force | Out-Null

Write-Host "release: $tag"
Write-Host "dir:     $runDir"

gh release download $tag `
  --repo $Repo `
  --pattern $binName `
  --pattern $shaName `
  --dir $runDir `
  --clobber

$binPath = Join-Path $runDir $binName
$shaPath = Join-Path $runDir $shaName

if (-not (Test-Path $binPath)) {
  Fail "downloaded binary not found: $binPath"
}
if (-not (Test-Path $shaPath)) {
  Fail "downloaded checksum not found: $shaPath"
}

$line = (Get-Content $shaPath | Select-Object -First 1).Trim()
if ([string]::IsNullOrWhiteSpace($line)) {
  Fail "checksum file is empty: $shaPath"
}
$m = [regex]::Match($line, '[0-9a-fA-F]{64}')
if (-not $m.Success) {
  Fail "invalid sha256 file: $shaPath"
}
$expected = $m.Value.ToLowerInvariant()
$actual = (Get-FileHash -Path $binPath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($expected -ne $actual) {
  Fail "sha256 mismatch for $binName"
}
Write-Host "$binName`: OK"

if ($DownloadOnly) {
  Write-Host "download complete: $binPath"
  exit 0
}

Push-Location $runDir
try {
  Write-Host "starting: $binName"
  & ".\$binName" @AppArgs
} finally {
  Pop-Location
}
