param(
  [ValidateSet('all', 'login', 'server')]
  [string]$Target = 'all'
)

$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Write-Host "Repo: $repoRoot"

function Build-Native {
  param([string]$Out, [string]$Pkg)

  $go = Get-Command go -ErrorAction SilentlyContinue
  if (-not $go) {
    throw "未检测到 Go（go 命令不存在）。请安装 Go，或改用 build-windows.cmd 通过 Docker 构建。"
  }

  Write-Host "go build -> $Out ($Pkg)"
  & go build -trimpath -ldflags "-s -w" -o $Out $Pkg
}

Set-Location $repoRoot

switch ($Target) {
  'login' { Build-Native -Out 'bilibili-login.exe' -Pkg './cmd/login' }
  'server' { Build-Native -Out 'bilibili-mcp.exe' -Pkg './cmd/server' }
  'all' {
    Build-Native -Out 'bilibili-login.exe' -Pkg './cmd/login'
    Build-Native -Out 'bilibili-mcp.exe' -Pkg './cmd/server'
  }
}

Write-Host "Done."
