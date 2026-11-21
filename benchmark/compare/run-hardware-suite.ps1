#requires -RunAsAdministrator

param(
    [int]$Seconds = 8,
    [switch]$Focused
)

$ErrorActionPreference = "Stop"
$outputDir = Join-Path $PSScriptRoot "profiles"
New-Item -ItemType Directory -Force $outputDir | Out-Null
$transcript = Join-Path $outputDir "hardware-suite.log"

Start-Transcript -Path $transcript -Force
$restoreKernelLogger = $false
try {
    logman query -ets "NT Kernel Logger" *> $null
    if ($LASTEXITCODE -eq 0) {
        xperf -stop
        if ($LASTEXITCODE -ne 0) {
            throw "failed to stop existing NT Kernel Logger"
        }
        $restoreKernelLogger = $true
    }
    if ($Focused) {
        $runs = @()
        foreach ($library in @("jjs-go", "sonic", "goccy")) {
            foreach ($counterSet in @("IPC", "LLC", "BranchRate")) {
                $runs += @{ Library = $library; Direction = "Encoder"; CounterSet = $counterSet }
            }
        }
        foreach ($counterSet in @("IPC", "LLC", "BranchRate")) {
            $runs += @{ Library = "jjs-go"; Direction = "Decoder"; CounterSet = $counterSet }
        }
    } else {
        $runs = @(
            @{ Library = "jjs-go"; Direction = "Encoder"; CounterSet = "Interactions" },
            @{ Library = "jjs-go"; Direction = "Encoder"; CounterSet = "Cache" },
            @{ Library = "jjs-go"; Direction = "Encoder"; CounterSet = "Branches" },
            @{ Library = "sonic"; Direction = "Encoder"; CounterSet = "Interactions" },
            @{ Library = "goccy"; Direction = "Encoder"; CounterSet = "Interactions" },
            @{ Library = "jjs-go"; Direction = "Decoder"; CounterSet = "Interactions" }
        )
    }
    foreach ($run in $runs) {
        Write-Host "=== $($run.Direction) / $($run.Library) / $($run.CounterSet) ==="
        & (Join-Path $PSScriptRoot "profile-hardware.ps1") `
            -Library $run.Library `
            -Direction $run.Direction `
            -CounterSet $run.CounterSet `
            -Seconds $Seconds `
            -UseDefaultKernelLogger
    }
} finally {
    if ($restoreKernelLogger) {
        xperf -start -on PROC_THREAD `
            -f "C:\Windows\System32\Logfiles\WMI\NT Kernel Logger.etl" `
            -BufferSize 4 `
            -FileMode Circular `
            -MaxFile 100 `
            -FlushTimer 3
    }
    Stop-Transcript
}
