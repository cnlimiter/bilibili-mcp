param(
  [switch]$Force,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'

Write-Host "Whisper.cpp model download script (Windows)"
Write-Host "==========================================="

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$modelsDir = Join-Path $repoRoot 'models'
$modelFile = Join-Path $modelsDir 'ggml-base.bin'
$modelUrl = 'https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin?download=true'

function Invoke-Download {
  param(
    [Parameter(Mandatory = $true)][string]$Url,
    [Parameter(Mandatory = $true)][string]$OutFile
  )

  $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
  if ($curl) {
    & $curl.Source -L --fail --retry 3 --retry-delay 2 -o $OutFile $Url
    return
  }

  $ProgressPreference = 'SilentlyContinue'
  $iwr = Get-Command Invoke-WebRequest -ErrorAction SilentlyContinue
  if (-not $iwr) {
    throw 'Missing curl.exe or Invoke-WebRequest; cannot download model file.'
  }

  if ($PSVersionTable.PSVersion.Major -lt 6) {
    Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $OutFile
  } else {
    Invoke-WebRequest -Uri $Url -OutFile $OutFile
  }
}

Write-Host "Models dir: $modelsDir"
if ($DryRun) {
  Write-Host "[DryRun] Will create: $modelsDir"
  Write-Host "[DryRun] Will download: $modelUrl"
  Write-Host "[DryRun] Output to: $modelFile"
  exit 0
}

New-Item -ItemType Directory -Force -Path $modelsDir | Out-Null

Write-Host ""
Write-Host "Downloading base model..."

if ((Test-Path $modelFile) -and (-not $Force)) {
  Write-Host "ggml-base.bin already exists: $modelFile"
} else {
  if (Test-Path $modelFile) {
    Remove-Item -Force $modelFile
  }
  Write-Host "Downloading ggml-base.bin (~142MB)..."
  Invoke-Download -Url $modelUrl -OutFile $modelFile
  Write-Host "Download complete: ggml-base.bin"
}

Write-Host ""
Write-Host "Done."
Write-Host ""
Write-Host "Files in models directory:"
Get-ChildItem -Path $modelsDir | Sort-Object Name | Format-Table Name, Length

Write-Host ""
Write-Host "Notes:"
Write-Host "  - ggml-base.bin: balanced speed/quality"

Write-Host ""
Write-Host "You can run whisper-init to initialize whisper.cpp"
