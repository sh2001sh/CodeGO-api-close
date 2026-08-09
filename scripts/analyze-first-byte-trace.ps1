param(
    [string]$HostName = "root@38.76.221.117",
    [string]$KeyPath = "$env:USERPROFILE\.ssh\codego-prod",
    [int]$SinceMinutes = 10,
    [switch]$AsJson
)

function Get-Percentile {
    param([double[]]$Values, [double]$Percentile)

    if ($Values.Count -eq 0) { return 0 }
    $sorted = @($Values | Sort-Object)
    $index = [math]::Ceiling(($sorted.Count - 1) * $Percentile)
    return $sorted[$index]
}

function Format-StageSummary {
    param([string]$Label, [object[]]$Records)

    $metrics = @("ingress_ms", "request_validation_ms", "admission_ms", "relay_info_ms", "preflight_ms", "route_selection_ms", "dispatch_ms", "upstream_first_event_ms", "total_ms")
    $summary = [ordered]@{ scope = $Label; samples = $Records.Count }
    foreach ($metric in $metrics) {
        $values = @($Records | ForEach-Object { [double]$_.trace.$metric })
        $summary["${metric}_p50"] = [math]::Round((Get-Percentile $values 0.50), 0)
        $summary["${metric}_p95"] = [math]::Round((Get-Percentile $values 0.95), 0)
    }
    return [pscustomobject]$summary
}

$remoteCommand = "docker logs --since ${SinceMinutes}m new-api-v2-gateway 2>&1"
$logLines = ssh -i "$KeyPath" -o BatchMode=yes -o ConnectTimeout=30 "$HostName" $remoteCommand
$records = foreach ($line in $logLines) {
    $marker = $line.IndexOf("params=")
    if ($marker -lt 0) { continue }
    try {
        $entry = $line.Substring($marker + 7) | ConvertFrom-Json
    } catch {
        continue
    }
    if ($entry.model_name -notlike "gpt-5.*" -or $null -eq $entry.other.first_byte_trace) { continue }
    [pscustomobject]@{
        channel = $entry.channel_id
        key     = $entry.other.admin_info.multi_key_index
        model   = $entry.model_name
        affinity = $entry.other.admin_info.route_decision.affinity_hit
        prompt_tokens = $entry.prompt_tokens
        trace   = $entry.other.first_byte_trace
    }
}

if ($records.Count -eq 0) {
    Write-Error "No GPT first-byte trace records were found in the requested window."
    exit 1
}

$allSummary = Format-StageSummary "all-gpt" @($records)
$groupSummaries = @($records |
    Group-Object { "channel=$($_.channel);key=$($_.key);affinity=$($_.affinity)" } |
    ForEach-Object { Format-StageSummary $_.Name @($_.Group) } |
    Sort-Object samples -Descending)

if ($AsJson) {
    [pscustomobject]@{ all = $allSummary; groups = $groupSummaries } | ConvertTo-Json -Depth 4
    exit 0
}

$allSummary | Format-List
$groupSummaries | Format-List
