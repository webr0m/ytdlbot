# scripts/update-tools.ps1
# Actualiza todas las aplicaciones y motores que usa el proyecto.
# Ejecutar con: pwsh -File scripts/update-tools.ps1
param([switch]$CheckOnly)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot

function Test-Command($name) {
    $null -ne (Get-Command $name -ErrorAction SilentlyContinue)
}

function Invoke-Update($name, $command, $args) {
    if (-not (Test-Command $name)) {
        Write-Host "  [SKIP] $name no esta instalado" -ForegroundColor Yellow
        return
    }
    if ($CheckOnly) {
        Write-Host "  [OK]   $name disponible" -ForegroundColor Green
        return
    }
    Write-Host "  [UPD]  $name..." -ForegroundColor Cyan
    try {
        & $command @args 2>&1 | Out-Null
        Write-Host "  [OK]   $name actualizado" -ForegroundColor Green
    } catch {
        Write-Host "  [ERR]  $name: $_" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "=== Actualizando herramientas del proyecto ===" -ForegroundColor Magenta
Write-Host ""

# --- yt-dlp (Python) ---
if (Test-Command "yt-dlp") {
    if ($CheckOnly) { Write-Host "  [OK]   yt-dlp disponible" -ForegroundColor Green }
    else {
        Write-Host "  [UPD]  yt-dlp..." -ForegroundColor Cyan
        try {
            if (Test-Command "pip") { & pip install --upgrade --no-cache-dir "yt-dlp[default]" 2>&1 | Out-Null }
            elseif (Test-Command "python") { & python -m pip install --upgrade --no-cache-dir "yt-dlp[default]" 2>&1 | Out-Null }
            Write-Host "  [OK]   yt-dlp actualizado" -ForegroundColor Green
        } catch { Write-Host "  [ERR]  yt-dlp: $_" -ForegroundColor Red }
    }
} else { Write-Host "  [SKIP] yt-dlp no esta instalado" -ForegroundColor Yellow }

# --- gallery-dl (Python) ---
if (Test-Command "gallery-dl") {
    if ($CheckOnly) { Write-Host "  [OK]   gallery-dl disponible" -ForegroundColor Green }
    else {
        Write-Host "  [UPD]  gallery-dl..." -ForegroundColor Cyan
        try {
            if (Test-Command "pip") { & pip install --upgrade --no-cache-dir gallery-dl 2>&1 | Out-Null }
            elseif (Test-Command "python") { & python -m pip install --upgrade --no-cache-dir gallery-dl 2>&1 | Out-Null }
            Write-Host "  [OK]   gallery-dl actualizado" -ForegroundColor Green
        } catch { Write-Host "  [ERR]  gallery-dl: $_" -ForegroundColor Red }
    }
} else { Write-Host "  [SKIP] gallery-dl no esta instalado" -ForegroundColor Yellow }

# --- aria2 (winget) ---
Invoke-Update "aria2c" "winget" @("upgrade", "--id", "aria2.aria2", "--accept-source-agreements", "--accept-package-agreements", "--silent")

# --- streamlink (winget) ---
Invoke-Update "streamlink" "winget" @("upgrade", "--id", "Streamlink.Streamlink", "--accept-source-agreements", "--accept-package-agreements", "--silent")

# --- N_m3u8DL-RE (winget) ---
Invoke-Update "N_m3u8DL-RE" "winget" @("upgrade", "--id", "nilaoda.N_m3u8DL-RE", "--accept-source-agreements", "--accept-package-agreements", "--silent")

# --- ffmpeg (winget) ---
Invoke-Update "ffmpeg" "winget" @("upgrade", "--id", "Gyan.FFmpeg", "--accept-source-agreements", "--accept-package-agreements", "--silent")

# --- Go modules ---
if (Test-Path -LiteralPath "go.mod") {
    if ($CheckOnly) { Write-Host "  [OK]   go.mod presente" -ForegroundColor Green }
    else {
        Write-Host "  [UPD]  modulos Go..." -ForegroundColor Cyan
        try {
            & .\.tools\go\bin\go.exe mod tidy 2>&1 | Out-Null
            Write-Host "  [OK]   modulos Go actualizados" -ForegroundColor Green
        } catch { Write-Host "  [ERR]  Go mod tidy: $_" -ForegroundColor Red }
    }
}

# --- npm dependencies ---
if (Test-Path -LiteralPath "web/package.json") {
    if ($CheckOnly) { Write-Host "  [OK]   web/package.json presente" -ForegroundColor Green }
    else {
        Write-Host "  [UPD]  dependencias npm..." -ForegroundColor Cyan
        try {
            Push-Location web
            & npm update 2>&1 | Out-Null
            Pop-Location
            Write-Host "  [OK]   dependencias npm actualizadas" -ForegroundColor Green
        } catch { Write-Host "  [ERR]  npm update: $_" -ForegroundColor Red; Pop-Location }
    }
}

Write-Host ""
Write-Host "=== Verificacion de motores ===" -ForegroundColor Magenta
$engines = @("yt-dlp", "gallery-dl", "aria2c", "streamlink", "N_m3u8DL-RE", "ffmpeg", "ffprobe")
foreach ($e in $engines) {
    if (Test-Command $e) {
        $ver = & $e --version 2>&1 | Select-Object -First 1
        Write-Host "  [OK]   $e -> $ver" -ForegroundColor Green
    } else {
        Write-Host "  [MISS] $e no instalado" -ForegroundColor Yellow
    }
}
Write-Host ""
Write-Host "Listo. Reinicia el servicio si cambiaron motores: pwsh -File scripts/stop.ps1; pwsh -File scripts/start.ps1 -Background" -ForegroundColor Magenta
Write-Host ""
