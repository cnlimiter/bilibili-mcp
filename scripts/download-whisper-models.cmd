@echo off
setlocal enableextensions

REM Windows wrapper for download-whisper-models.ps1
REM Usage:
REM   scripts\download-whisper-models.cmd
REM   scripts\download-whisper-models.cmd -Force
REM   scripts\download-whisper-models.cmd -DryRun

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0download-whisper-models.ps1" %*
