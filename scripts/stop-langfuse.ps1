# Stop Langfuse and all dependencies.
$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$ComposeFile = Join-Path $ProjectDir "docker-compose.langfuse.yml"

try {
    docker compose version 2>$null | Out-Null
    $cmd = "docker compose"
} catch {
    if (Get-Command docker-compose -ErrorAction SilentlyContinue) {
        $cmd = "docker-compose"
    } else {
        Write-Host "Docker Compose not found." -ForegroundColor Red
        exit 1
    }
}

Invoke-Expression "$cmd -f `"$ComposeFile`" down"
Write-Host "Langfuse stopped." -ForegroundColor Green
