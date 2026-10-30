param([switch]$Background)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$env:PATH = (Join-Path $projectRoot '.tools') + ';' + (Join-Path $projectRoot '.tools\engines\Scripts') + ';' + $env:PATH
if (-not (Test-Path -LiteralPath 'bin\webrom-downloader.exe')) { throw 'Ejecuta scripts/setup.ps1 primero' }
docker compose up -d --wait postgres
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL no disponible' }
if ($Background) {
  $process = Start-Process -FilePath (Join-Path $projectRoot 'bin\webrom-downloader.exe') -WorkingDirectory $projectRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $projectRoot 'data\server.out.log') -RedirectStandardError (Join-Path $projectRoot 'data\server.err.log')
  Set-Content -LiteralPath 'data\server.pid' -Value $process.Id
  Write-Host ('Servicio iniciado, PID ' + $process.Id + '. Panel: http://127.0.0.1:8095')
} else { & '.\bin\webrom-downloader.exe' }
