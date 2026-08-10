param(
    [Parameter(Mandatory = $true)]
    [string]$SourceFile,
    [Parameter(Mandatory = $true)]
    [string]$OutputRoot,
    [string]$OnlyHost = '',
    [string]$ModelOverride = '',
    [string]$PromptOverride = 'Reply with exactly: TOKEN_AUDIT_OK'
)

$ErrorActionPreference = 'Stop'

function Write-Utf8NoBom {
    param([string]$Path, [string]$Content)

    $encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Content, $encoding)
}

function Get-ErrorResponseBody {
    param([System.Management.Automation.ErrorRecord]$ErrorRecord)

    $response = $ErrorRecord.Exception.Response
    if ($null -eq $response) {
        return $ErrorRecord.Exception.Message
    }

    $reader = New-Object System.IO.StreamReader($response.GetResponseStream())
    try {
        return $reader.ReadToEnd()
    } finally {
        $reader.Dispose()
    }
}

function Invoke-UpstreamRequest {
    param(
        [string]$Uri,
        [hashtable]$Headers,
        [string]$Body
    )

    try {
        $response = Invoke-WebRequest -UseBasicParsing -Method Post -Uri $Uri -Headers $Headers -ContentType 'application/json' -Body $Body -TimeoutSec 120
        return [pscustomobject]@{
            StatusCode = [int]$response.StatusCode
            Body = [string]$response.Content
            Error = $null
        }
    } catch {
        return [pscustomobject]@{
            StatusCode = if ($null -ne $_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { 0 }
            Body = Get-ErrorResponseBody $_
            Error = $_.Exception.Message
        }
    }
}

function Get-StreamSummary {
    param([string]$Body)

    $text = New-Object System.Text.StringBuilder
    $usage = $null
    foreach ($line in ($Body -split "`r?`n")) {
        if (-not $line.StartsWith('data:')) {
            continue
        }
        $payload = $line.Substring(5).Trim()
        if ($payload.Length -eq 0 -or $payload -eq '[DONE]') {
            continue
        }
        try {
            $event = $payload | ConvertFrom-Json
            if ($null -ne $event.usage) {
                $usage = $event.usage
            }
            if ($null -ne $event.choices -and $event.choices.Count -gt 0 -and $null -ne $event.choices[0].delta.content) {
                [void]$text.Append([string]$event.choices[0].delta.content)
            }
        } catch {
            continue
        }
    }
    return [pscustomobject]@{
        VisibleText = $text.ToString()
        Usage = $usage
    }
}

$entries = @()
$current = $null
foreach ($rawLine in Get-Content -LiteralPath $SourceFile) {
    $line = $rawLine.Trim()
    if ($line.Length -eq 0) {
        continue
    }
    if ($line -match '^https?://') {
        $current = [pscustomobject]@{ BaseUrl = $line.TrimEnd('/'); Keys = @() }
        $entries += $current
        continue
    }
    if ($line.StartsWith('sk-') -and $null -ne $current) {
        $current.Keys += $line
    }
}

if ($entries.Count -eq 0) {
    throw 'No upstream endpoint/key pairs were found.'
}

$timestamp = Get-Date -Format 'yyyyMMddTHHmmss'
$runDirectory = Join-Path $OutputRoot "upstream-token-audit-$timestamp"
New-Item -ItemType Directory -Force -Path $runDirectory | Out-Null

$modelByHost = @{
    'tokunex.com' = 'gpt-5.6-terra'
    'apiok.cc' = 'gpt-5.4'
    'api.relaylink.cloud' = 'gpt-5.6-terra'
}
$manifest = @()

foreach ($entry in $entries) {
    $apiHost = ([Uri]$entry.BaseUrl).Host.ToLowerInvariant()
    if ($OnlyHost.Length -gt 0 -and $apiHost -ne $OnlyHost.ToLowerInvariant()) {
        continue
    }
    if (-not $modelByHost.ContainsKey($apiHost)) {
        throw "No test model configured for $apiHost."
    }
    $model = if ($ModelOverride.Length -gt 0) { $ModelOverride } else { $modelByHost[$apiHost] }
    for ($index = 0; $index -lt $entry.Keys.Count; $index++) {
        $label = "$($apiHost.Replace('.', '-'))-key-$($index + 1)"
        $caseDirectory = Join-Path $runDirectory $label
        New-Item -ItemType Directory -Force -Path $caseDirectory | Out-Null

        $headers = @{
            Authorization = "Bearer $($entry.Keys[$index])"
            Accept = 'application/json'
            'User-Agent' = 'CodeGo-upstream-token-audit/1.0'
        }
        $messages = @([ordered]@{ role = 'user'; content = $PromptOverride })
        $normalRequest = [ordered]@{
            model = $model
            stream = $false
            messages = $messages
            max_tokens = 64
            temperature = 0
        }
        $streamRequest = [ordered]@{
            model = $model
            stream = $true
            stream_options = [ordered]@{ include_usage = $true }
            messages = $messages
            max_tokens = 64
            temperature = 0
        }
        $normalBody = $normalRequest | ConvertTo-Json -Depth 8
        $streamBody = $streamRequest | ConvertTo-Json -Depth 8
        Write-Utf8NoBom (Join-Path $caseDirectory 'request.json') $normalBody
        Write-Utf8NoBom (Join-Path $caseDirectory 'stream-request.json') $streamBody

        $normal = Invoke-UpstreamRequest "$($entry.BaseUrl)/chat/completions" $headers $normalBody
        Write-Utf8NoBom (Join-Path $caseDirectory 'response.json') $normal.Body

        $headers.Accept = 'text/event-stream'
        $stream = Invoke-UpstreamRequest "$($entry.BaseUrl)/chat/completions" $headers $streamBody
        Write-Utf8NoBom (Join-Path $caseDirectory 'stream-response.sse') $stream.Body

        $normalJson = $null
        try { $normalJson = $normal.Body | ConvertFrom-Json } catch { }
        $streamSummary = Get-StreamSummary $stream.Body
        $summary = [ordered]@{
            endpoint = $entry.BaseUrl
            model = $model
            non_stream_status_code = $normal.StatusCode
            non_stream_error = $normal.Error
            non_stream_visible_text = if ($null -ne $normalJson) { $normalJson.choices[0].message.content } else { $null }
            non_stream_usage = if ($null -ne $normalJson) { $normalJson.usage } else { $null }
            stream_status_code = $stream.StatusCode
            stream_error = $stream.Error
            stream_visible_text = $streamSummary.VisibleText
            stream_usage = $streamSummary.Usage
        }
        Write-Utf8NoBom (Join-Path $caseDirectory 'summary.json') ($summary | ConvertTo-Json -Depth 10)
        $manifest += [ordered]@{
            case = $label
            endpoint = $entry.BaseUrl
            model = $model
            summary_file = "$label/summary.json"
        }
    }
}

Write-Utf8NoBom (Join-Path $runDirectory 'manifest.json') ($manifest | ConvertTo-Json -Depth 6)
Write-Output $runDirectory
