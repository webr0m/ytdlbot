param([switch]$SkipEngines)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
New-Item -ItemType Directory -Force .tools,bin,data,secrets | Out-Null
if (-not (Test-Path -LiteralPath '.env')) {
  $databasePassword = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLowerInvariant()
  $localPanelKey = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLowerInvariant()
  $envText = [IO.File]::ReadAllText((Join-Path $projectRoot '.env.example'))
  $envText = $envText.Replace('CAMBIAR', $databasePassword).Replace('LOCAL_ADMIN_KEY=', 'LOCAL_ADMIN_KEY=' + $localPanelKey).Replace('ADMIN_IDS=', 'ADMIN_IDS=1').Replace('PUBLIC_URL=', 'PUBLIC_URL=https://api.webrom.cc')
  [IO.File]::WriteAllText((Join-Path $projectRoot '.env'), $envText)
  Write-Host 'Creado .env con claves aleatorias. Reemplaza ADMIN_IDS=1 por tu ID antes de activar Telegram.'
}
$goBin = Join-Path $projectRoot '.tools\go\bin\go.exe'
if (-not (Test-Path -LiteralPath $goBin)) {
  $goRelease = (Invoke-RestMethod 'https://go.dev/dl/?mode=json')[0]
  $goArchive = $goRelease.files | Where-Object { $_.os -eq 'windows' -and $_.arch -eq 'amd64' -and $_.kind -eq 'archive' } | Select-Object -First 1
  Invoke-WebRequest ('https://go.dev/dl/' + $goArchive.filename) -OutFile '.tools\go.zip'
  if ((Get-FileHash '.tools\go.zip' -Algorithm SHA256).Hash.ToLowerInvariant() -ne $goArchive.sha256) { throw 'Checksum de Go inválido' }
  Expand-Archive -LiteralPath '.tools\go.zip' -DestinationPath '.tools' -Force
}
if (-not $SkipEngines) {
  Invoke-WebRequest 'https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe' -OutFile '.tools\yt-dlp.exe'
  Invoke-WebRequest 'https://github.com/yt-dlp/yt-dlp/releases/latest/download/SHA2-256SUMS' -OutFile '.tools\yt-dlp.sha256'
  $hashLine = Get-Content -LiteralPath '.tools\yt-dlp.sha256' | Where-Object { $_ -match '\s+yt-dlp\.exe$' } | Select-Object -First 1
  $expectedHash = ($hashLine -split '\s+')[0]
  if ((Get-FileHash '.tools\yt-dlp.exe' -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expectedHash) { throw 'Checksum yt-dlp inválido' }
  python -m venv .tools\engines
  if ($LASTEXITCODE -ne 0) { throw 'Python 3.11+ es necesario para gallery-dl' }
  & '.\.tools\engines\Scripts\python.exe' -m pip install --upgrade pip gallery-dl
  if ($LASTEXITCODE -ne 0) { throw 'No se pudo instalar gallery-dl' }
}
npm ci --prefix web
if ($LASTEXITCODE -ne 0) { throw 'Falló npm ci' }
npm run build --prefix web
if ($LASTEXITCODE -ne 0) { throw 'Falló la compilación web' }
& $goBin mod tidy
& $goBin build -trimpath -o bin\webrom-downloader.exe ./cmd/server
if ($LASTEXITCODE -ne 0) { throw 'Falló la compilación Go' }
docker compose up -d --wait postgres
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL no arrancó; comprueba Docker Desktop' }
Write-Host 'Listo. Ejecuta scripts/start.ps1. Las credenciales siguen en .env y no se muestran.'
