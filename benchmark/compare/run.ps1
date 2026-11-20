param(
    [string]$Bench = "^(BenchmarkEncoder_|BenchmarkDecoder_)",
    [string]$BenchTime = "2s",
    [int]$Count = 3
)

$ErrorActionPreference = "Stop"
$env:SONIC_NO_ASYNC_GC = "1"

go test -run '^$' `
    -bench $Bench `
    -benchmem `
    -benchtime $BenchTime `
    -count $Count
