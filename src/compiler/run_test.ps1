$result = Get-Content 'D:\SWC\CandorSWC\src\compiler\test.cnd' -Raw | & 'D:\SWC\CandorSWC\src\compiler\lexer.exe'
$ec = $LASTEXITCODE
Write-Host "EXIT: $ec"
Write-Host "OUTPUT:"
Write-Host $result
