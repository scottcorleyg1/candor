# Test if the crash happens in lex, parse, or emit_c
# by creating a minimal test program that calls each phase
$src = @"
fn main() -> unit { print("hello from candor") }
"@

# Write to temp file
$src | Out-File -FilePath 'D:\SWC\CandorSWC\src\compiler\test_input.cnd' -Encoding utf8 -NoNewline

Write-Host "Running bootstrap binary..."
$result = Get-Content 'D:\SWC\CandorSWC\src\compiler\test_input.cnd' -Raw | & 'D:\SWC\CandorSWC\src\compiler\lexer.exe' 2>&1
$ec = $LASTEXITCODE
Write-Host "EXIT: $ec"
Write-Host "OUTPUT LENGTH: $($result.Length)"
if ($result.Length -gt 0) {
    Write-Host "OUTPUT (first 500 chars):"
    Write-Host $result.Substring(0, [Math]::Min(500, $result.Length))
} else {
    Write-Host "No output produced"
}
