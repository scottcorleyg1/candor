# Test with empty function
$t1 = "fn foo() -> unit { }"
Write-Host "=== Empty function ==="
$r = $t1 | & 'D:\SWC\CandorSWC\src\compiler\lexer.exe'
Write-Host "Exit: $LASTEXITCODE"
Write-Host "Output: $r"

# Test with return unit
$t2 = "fn foo() -> unit { return unit }"
Write-Host "=== Return unit ==="
$r = $t2 | & 'D:\SWC\CandorSWC\src\compiler\lexer.exe'
Write-Host "Exit: $LASTEXITCODE"
Write-Host "Output: $r"

# Test with expression statement (no call)
$t3 = "fn foo() -> unit { 42 }"
Write-Host "=== Expr stmt int ==="
$r = $t3 | & 'D:\SWC\CandorSWC\src\compiler\lexer.exe'
Write-Host "Exit: $LASTEXITCODE"
Write-Host "Output: $r"

# Test with simple function call (known builtin)
$t4 = 'fn foo() -> unit { print("hi") }'
Write-Host "=== Print call ==="
$r = $t4 | & 'D:\SWC\CandorSWC\src\compiler\lexer.exe'
Write-Host "Exit: $LASTEXITCODE"
Write-Host "Output: $r"
