param(
    [Parameter(Mandatory = $true)]
    [string]$Etl,
    [string]$ProcessName = "compare.test.exe"
)

$ErrorActionPreference = "Stop"
$etlPath = (Resolve-Path $Etl).Path
$markText = & xperf -i $etlPath -a marks -summary 2>&1
$startMatch = [regex]::Match(($markText -join "`n"), 'Mark,\s*(\d+),\s*JJS_START_')
$stopMatch = [regex]::Match(($markText -join "`n"), 'Mark,\s*(\d+),\s*JJS_STOP_')
if (-not $startMatch.Success -or -not $stopMatch.Success) {
    throw "JJS start/stop marks were not found in $etlPath"
}
$start = [int64]$startMatch.Groups[1].Value
$stop = [int64]$stopMatch.Groups[1].Value

$counters = @{}
$functions = @{}
$linePattern = '^\s*PmcInterrupt,\s*(\d+),\s*(\d+),\s*(.+?)\s+\(\s*\d+\),\s*(.+?),\s*([^,]+),\s*0\s*$'

foreach ($line in (& xperf -i $etlPath -a pmc -range $start $stop 2>&1)) {
    $match = [regex]::Match($line, $linePattern)
    if (-not $match.Success -or $match.Groups[3].Value -notlike "*$ProcessName*") {
        continue
    }
    $function = $match.Groups[4].Value.Trim()
    $source = $match.Groups[5].Value.Trim()
    if (-not $counters.ContainsKey($source)) {
        $counters[$source] = 0
    }
    $counters[$source]++
    $functionKey = "$source`t$function"
    if (-not $functions.ContainsKey($functionKey)) {
        $functions[$functionKey] = 0
    }
    $functions[$functionKey]++
}

$instructions = [double]$counters["InstructionRetired"]
$cycles = [double]$counters["UnhaltedCoreCycles"]
$llcMisses = [double]$counters["LLCMisses"]
$branchMisses = [double]$counters["BranchMispredictsRetired"]
$branches = [double]$counters["BranchInstructionRetired"]

[pscustomobject]@{
    Etl = $etlPath
    Process = $ProcessName
    Start = $start
    Stop = $stop
    InstructionRetired = $instructions
    UnhaltedCoreCycles = $cycles
    LLCMisses = $llcMisses
    BranchMispredictsRetired = $branchMisses
    BranchInstructionRetired = $branches
    IPC = if ($cycles) { $instructions / $cycles } else { $null }
    LLCMPKI = if ($instructions) { $llcMisses * 1000 / $instructions } else { $null }
    BranchMPKI = if ($instructions) { $branchMisses * 1000 / $instructions } else { $null }
    BranchMispredictRate = if ($branches) { $branchMisses / $branches } else { $null }
}

$base = [IO.Path]::Combine(
    [IO.Path]::GetDirectoryName($etlPath),
    [IO.Path]::GetFileNameWithoutExtension($etlPath)
)
$functions.GetEnumerator() |
    ForEach-Object {
        $parts = $_.Key -split "`t", 2
        [pscustomobject]@{
            Counter = $parts[0]
            Function = $parts[1]
            Samples = $_.Value
        }
    } |
    Sort-Object Counter, Samples -Descending |
    Export-Csv -NoTypeInformation -Path ($base + "-functions.csv")
