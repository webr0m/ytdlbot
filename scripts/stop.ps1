$projectRoot = Split-Path -Parent $PSScriptRoot
$pidFile = Join-Path $projectRoot 'data\server.pid'
if (Test-Path -LiteralPath $pidFile) {
 $serverPid = [int](Get-Content -LiteralPath $pidFile)
 $process = Get-Process -Id $serverPid -ErrorAction SilentlyContinue
 if ($process -and $process.Path -eq (Join-Path $projectRoot 'bin\webrom-downloader.exe')) {
   Stop-Process -Id $serverPid -ErrorAction Stop
   if (-not $process.WaitForExit(10000)) { throw 'El servicio no terminó; no reemplaces el ejecutable todavía.' }
 }
}
Write-Host 'Detenido el servicio del proyecto. No se detuvieron Docker ni otros servicios del equipo.'
