param([switch]$MigrateFromCloud)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$values = @{}
foreach ($line in Get-Content -LiteralPath '.env') { if ($line -match '^([A-Z_]+)=(.*)$') { $values[$Matches[1]] = $Matches[2].Trim().Trim('"').Trim("'") } }
foreach ($key in @('BOT_TOKEN','TELEGRAM_API_ID','TELEGRAM_API_HASH','ADMIN_IDS')) { if (-not $values[$key]) { throw ('Falta ' + $key + ' en .env') } }
if ($values['ADMIN_IDS'] -eq '1') { throw 'Reemplaza ADMIN_IDS=1 por tu ID de Telegram real' }
if ($MigrateFromCloud) {
  # Solo usar si el bot ya trabaja con la API cloud. No imprime la URL/token.
  $migration = Invoke-RestMethod -Method Post -Uri ('https://api.telegram.org/bot' + $values['BOT_TOKEN'] + '/logOut')
  if (-not $migration.ok) { throw 'Telegram no confirmó logOut' }
}
docker compose --profile telegram up -d telegram-api
if ($LASTEXITCODE -ne 0) { throw 'No arrancó la API local de Telegram' }
Write-Host 'API local configurada en 127.0.0.1:8081. Reinicia el servicio del proyecto para leer .env.'
