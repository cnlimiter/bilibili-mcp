@echo off
setlocal enableextensions

REM Windows amd64 build helper.
REM If you have Go installed, prefer: scripts\build-windows.ps1
REM If you DO NOT have Go, this script builds via Docker.

set "REPO=%~dp0.."
set "IMG=mcr.microsoft.com/oss/go/microsoft/golang:1.21"

if "%1"=="" goto :all
if /I "%1"=="all" goto :all
if /I "%1"=="login" goto :login
if /I "%1"=="server" goto :server

echo Usage: %~nx0 [all^|login^|server]
exit /b 2

:login
call :build bilibili-login.exe ./cmd/login
goto :eof

:server
call :build bilibili-mcp.exe ./cmd/server
goto :eof

:all
call :build bilibili-login.exe ./cmd/login
call :build bilibili-mcp.exe ./cmd/server
goto :eof

:build
set "OUT=%~1"
set "PKG=%~2"

echo Building %OUT% from %PKG% ...
docker run --rm -e GOOS=windows -e GOARCH=amd64 -e CGO_ENABLED=0 -v "%cd%:/src" -w /src %IMG% go build -trimpath -ldflags "-s -w" -o %OUT% %PKG%
if errorlevel 1 exit /b 1

echo OK: %OUT%
exit /b 0
