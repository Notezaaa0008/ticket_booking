# Runs every standard check (Windows PowerShell). Paste only failures into Cursor.
# Use $env:NO_RACE = "1" if the Go race detector is unavailable.
Set-Location (Join-Path $PSScriptRoot "..")
$failed = @()

function Run($name, $dir, [scriptblock]$cmd) {
  Write-Host "=== $name"
  Push-Location $dir
  & $cmd
  $code = $LASTEXITCODE
  Pop-Location
  if ($code -ne 0) { Write-Host "FAILED: $name" -ForegroundColor Red; $script:failed += $name }
  else { Write-Host "OK: $name" -ForegroundColor Green }
}

Run "go vet" "backend" { go vet ./... }
Run "gofmt" "backend" { $o = gofmt -l .; if ($o) { $o; cmd /c exit 1 } else { cmd /c exit 0 } }
Run "go test" "backend" { if ($env:NO_RACE) { go test ./... -count=1 } else { go test ./... -race -count=1 } }
if (Test-Path "frontend/package.json") {
  Run "eslint" "frontend" { npm run lint }
  Run "tsc" "frontend" { npx tsc --noEmit }
  Run "next build" "frontend" { npm run build }
}

Write-Host ""
if ($failed.Count -eq 0) { Write-Host "ALL CHECKS PASSED" -ForegroundColor Green }
else { Write-Host ("FAILED: " + ($failed -join ", ")) -ForegroundColor Red; exit 1 }
