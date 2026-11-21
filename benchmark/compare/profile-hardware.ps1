#requires -RunAsAdministrator

param(
    [ValidateSet("jjs-go", "sonic", "goccy", "jsoniter", "stdlib", "easyjson")]
    [string]$Library = "jjs-go",
    [ValidateSet("Encoder", "Decoder")]
    [string]$Direction = "Encoder",
    [int]$Seconds = 10,
    [ValidateSet("Interactions", "Branches", "Cache")]
    [string]$CounterSet = "Interactions"
)

$ErrorActionPreference = "Stop"
$env:SONIC_NO_ASYNC_GC = "1"

switch ($CounterSet) {
    "Interactions" {
        $counters = "LLCMisses,BranchMispredictsRetired,InstructionRetired,UnhaltedCoreCycles"
    }
    "Branches" {
        $counters = "BranchInstructionRetired,BranchMispredictsRetired,InstructionRetired,UnhaltedCoreCycles"
    }
    "Cache" {
        $counters = "CacheMisses,LLCMisses,InstructionRetired,UnhaltedCoreCycles"
    }
}

$bench = "^Benchmark${Direction}_Binding/Large/$Library$"
$outputDir = Join-Path $PSScriptRoot "profiles"
New-Item -ItemType Directory -Force $outputDir | Out-Null
$testBinary = Join-Path $outputDir "compare.test.exe"
$etl = Join-Path $outputDir "${Direction}-${Library}-${CounterSet}.etl"
$session = "jjs-pmc-$PID"

Push-Location $PSScriptRoot
try {
    go test -c -o $testBinary
    if ($LASTEXITCODE -ne 0) {
        throw "failed to build benchmark test binary"
    }

    # Compile JIT plans and populate caches before starting hardware sampling.
    & $testBinary -test.run '^$' -test.bench $bench -test.benchtime 100ms -test.count 1
    if ($LASTEXITCODE -ne 0) {
        throw "benchmark warmup failed"
    }

    xperf -start $session `
        -on PROC_THREAD+LOADER+PROFILE `
        -stackwalk Profile `
        -PmcProfile $counters `
        -f $etl
    if ($LASTEXITCODE -ne 0) {
        throw "failed to start PMU trace; check VTune/xperf drivers and hypervisor PMU exposure"
    }

    try {
        & $testBinary -test.run '^$' -test.bench $bench `
            -test.benchtime "${Seconds}s" -test.count 1
        if ($LASTEXITCODE -ne 0) {
            throw "benchmark failed while recording PMU trace"
        }
    } finally {
        xperf -stop $session
    }
} finally {
    Pop-Location
}

Write-Host "ETL: $etl"
Write-Host "Open it in WPA and add CPU Usage (Sampled) with hardware counter columns."
Write-Host "Counters: $counters"
