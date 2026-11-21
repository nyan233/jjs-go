#requires -RunAsAdministrator

param(
    [ValidateSet("jjs-go", "sonic", "goccy", "jsoniter", "stdlib", "easyjson")]
    [string]$Library = "jjs-go",
    [ValidateSet("Encoder", "Decoder")]
    [string]$Direction = "Encoder",
    [int]$Seconds = 10,
    [ValidateSet("Interactions", "Branches", "Cache", "IPC", "LLC", "BranchRate")]
    [string]$CounterSet = "Interactions",
    [switch]$UseDefaultKernelLogger
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
    "IPC" {
        $counters = "InstructionRetired,UnhaltedCoreCycles"
    }
    "LLC" {
        $counters = "LLCMisses,InstructionRetired"
    }
    "BranchRate" {
        $counters = "BranchInstructionRetired,BranchMispredictsRetired"
    }
}

$bench = "^Benchmark${Direction}_Binding/Large/$Library$"
$outputDir = Join-Path $PSScriptRoot "profiles"
New-Item -ItemType Directory -Force $outputDir | Out-Null
$testBinary = Join-Path $outputDir "compare.test.exe"
$etl = Join-Path $outputDir "${Direction}-${Library}-${CounterSet}.etl"
$session = "jjs-pmc-$PID"
$kernelLogger = "NT Kernel Logger"
$kernelLoggerTrace = "C:\Windows\System32\Logfiles\WMI\NT Kernel Logger.etl"
$usingExistingKernelLogger = $false

Push-Location $PSScriptRoot
try {
    go test -c -o $testBinary
    if ($LASTEXITCODE -ne 0) {
        throw "failed to build benchmark test binary"
    }

    # Compile JIT plans and populate caches before starting hardware sampling.
    & $testBinary '-test.run' '^$' '-test.bench' $bench '-test.benchtime' '100ms' '-test.count' '1'
    if ($LASTEXITCODE -ne 0) {
        throw "benchmark warmup failed"
    }

    Remove-Item -LiteralPath $etl -Force -ErrorAction SilentlyContinue
    if ($UseDefaultKernelLogger) {
        xperf -start `
            -on PROC_THREAD+LOADER+CSWITCH+DPC+INTERRUPT+PMC_PROFILE `
            -stackwalk PMCINTERRUPT `
            -PmcProfile $counters `
            -f $etl `
            -BufferSize 64 `
            -MinBuffers 64 `
            -MaxBuffers 320 `
            -FileMode Sequential
        if ($LASTEXITCODE -ne 0) {
            throw "failed to configure PMU trace"
        }
        xperf -m "JJS_START_${Direction}_${Library}_${CounterSet}"
    } else {
        xperf -start $session `
            -on PROC_THREAD+LOADER+CSWITCH+DPC+INTERRUPT+PMC_PROFILE `
            -stackwalk PMCINTERRUPT `
            -PmcProfile $counters `
            -f $etl
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "Independent PMU session unavailable; updating the existing NT Kernel Logger."
            xperf -update $kernelLogger `
                -on PROC_THREAD+LOADER+CSWITCH+DPC+INTERRUPT+PMC_PROFILE `
                -stackwalk PMCINTERRUPT `
                -PmcProfile $counters
            if ($LASTEXITCODE -ne 0) {
                throw "failed to configure PMU trace"
            }
            $usingExistingKernelLogger = $true
            xperf -m "JJS_START_${Direction}_${Library}_${CounterSet}"
        }
    }

    try {
        & $testBinary '-test.run' '^$' '-test.bench' $bench `
            '-test.benchtime' "${Seconds}s" '-test.count' '1'
        if ($LASTEXITCODE -ne 0) {
            throw "benchmark failed while recording PMU trace"
        }
    } finally {
        if ($UseDefaultKernelLogger) {
            xperf -m "JJS_STOP_${Direction}_${Library}_${CounterSet}"
            xperf -stop
        } elseif ($usingExistingKernelLogger) {
            xperf -m "JJS_STOP_${Direction}_${Library}_${CounterSet}"
            xperf -flush $kernelLogger
            Copy-Item -LiteralPath $kernelLoggerTrace -Destination $etl -Force
        } else {
            xperf -stop $session
        }
    }
} finally {
    if ($usingExistingKernelLogger) {
        xperf -update $kernelLogger -on PROC_THREAD
    }
    Pop-Location
}

Write-Host "ETL: $etl"
Write-Host "Open it in WPA and add CPU Usage (Sampled) with hardware counter columns."
Write-Host "Counters: $counters"
