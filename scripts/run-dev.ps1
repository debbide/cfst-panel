$ErrorActionPreference = 'Stop'
Set-Location (Split-Path -Parent $PSScriptRoot)
go run .\cmd\server --addr 127.0.0.1:8787
