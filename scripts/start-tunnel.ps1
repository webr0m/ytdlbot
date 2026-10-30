$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$tokenPath = Join-Path $projectRoot 'secrets\cloudflare-token.txt'
if (-not (Test-Path -LiteralPath $tokenPath)) { throw 'Falta secrets/cloudflare-token.txt del túnel configurado' }
$tunnelProcess = Start-Process -FilePath 'C:\WINDOWS\system32\cloudflared.exe' -ArgumentList @('tunnel','--no-autoupdate','run','--token-file',$tokenPath) -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $projectRoot 'data\tunnel.log')
Set-Content -LiteralPath (Join-Path $projectRoot 'data\tunnel.pid') -Value $tunnelProcess.Id
Write-Host 'Conector Cloudflare iniciado. No se modificaron los otros procesos cloudflared.'
