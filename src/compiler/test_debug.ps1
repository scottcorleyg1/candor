$r = "fn foo() -> unit { return unit }" | & 'D:\SWC\CandorSWC\src\compiler\lexer_debug.exe' 2>&1
Write-Host "Exit: $LASTEXITCODE"
Write-Host "ALL OUTPUT:"
$r | ForEach-Object { Write-Host $_ }
