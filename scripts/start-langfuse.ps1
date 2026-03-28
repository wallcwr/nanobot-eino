# Start Langfuse and all dependencies via Docker Compose.
# Works on Windows (PowerShell). Auto-installs Docker if not found.

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir
$ComposeFile = Join-Path $ProjectDir "docker-compose.langfuse.yml"

function Write-Info  { param($msg) Write-Host "[INFO] $msg" -ForegroundColor Green }
function Write-Warn  { param($msg) Write-Host "[WARN] $msg" -ForegroundColor Yellow }
function Write-Err   { param($msg) Write-Host "[ERROR] $msg" -ForegroundColor Red; exit 1 }

# --- Docker installation ---
function Install-DockerWindows {
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Info "Installing Docker Desktop via winget..."
        winget install -e --id Docker.DockerDesktop --accept-source-agreements --accept-package-agreements
        Write-Warn "Docker Desktop installed. You may need to restart your computer and re-run this script."
        exit 0
    }
    elseif (Get-Command choco -ErrorAction SilentlyContinue) {
        Write-Info "Installing Docker Desktop via Chocolatey..."
        choco install docker-desktop -y
        Write-Warn "Docker Desktop installed. You may need to restart your computer and re-run this script."
        exit 0
    }
    else {
        Write-Err "No package manager found (winget/choco). Please install Docker Desktop manually: https://docs.docker.com/desktop/install/windows-install/"
    }
}

function Ensure-Docker {
    $dockerExists = Get-Command docker -ErrorAction SilentlyContinue
    if ($dockerExists) {
        try {
            docker info 2>$null | Out-Null
            if ($LASTEXITCODE -eq 0) {
                Write-Info "Docker is running."
                return
            }
        } catch {}

        Write-Warn "Docker is installed but the daemon is not running."
        Write-Info "Attempting to start Docker Desktop..."
        Start-Process "Docker Desktop" -ErrorAction SilentlyContinue
        $retries = 30
        while ($retries -gt 0) {
            Start-Sleep -Seconds 2
            try {
                docker info 2>$null | Out-Null
                if ($LASTEXITCODE -eq 0) {
                    Write-Info "Docker daemon is now running."
                    return
                }
            } catch {}
            $retries--
        }
        Write-Err "Could not start Docker daemon. Please start Docker Desktop manually."
    }

    Write-Warn "Docker is not installed."
    $answer = Read-Host "Install Docker Desktop automatically? [y/N]"
    if ($answer -match '^[yY]') {
        Install-DockerWindows
    }
    else {
        Write-Err "Docker is required. Install it from https://docs.docker.com/get-docker/"
    }
}

function Get-ComposeCmd {
    try {
        docker compose version 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            return "docker compose"
        }
    } catch {}

    if (Get-Command docker-compose -ErrorAction SilentlyContinue) {
        return "docker-compose"
    }

    Write-Err "Docker Compose not found. Please install Docker Compose: https://docs.docker.com/compose/install/"
}

# --- Main ---
Write-Info "=== Langfuse Startup Script ==="
Ensure-Docker
$ComposeCmd = Get-ComposeCmd
Write-Info "Using: $ComposeCmd"

Write-Info "Starting Langfuse services..."
Invoke-Expression "$ComposeCmd -f `"$ComposeFile`" up -d"

Write-Info "Waiting for Langfuse to be ready..."
$retries = 60
$ready = $false
while ($retries -gt 0) {
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:3000" -UseBasicParsing -TimeoutSec 3 -ErrorAction SilentlyContinue
        if ($response.StatusCode -eq 200) {
            $ready = $true
            break
        }
    } catch {}
    Start-Sleep -Seconds 3
    $retries--
}

Write-Host ""
if ($ready) {
    Write-Info "=== Langfuse is ready! ==="
    Write-Info "UI:            http://localhost:3000"
    Write-Info "MinIO Console: http://localhost:9090"
    Write-Host ""
    Write-Info "To stop:  $ComposeCmd -f $ComposeFile down"
    Write-Info "To logs:  $ComposeCmd -f $ComposeFile logs -f"
}
else {
    Write-Warn "Langfuse may still be starting. Check logs:"
    Write-Warn "  $ComposeCmd -f $ComposeFile logs -f langfuse"
}
