@echo off
setlocal enabledelayedexpansion

:: CodexForAll Windows setup script (ASCII only)
echo.
echo CodexForAll Windows Setup
echo ======================
echo.

set "CODEXFORALL_SERVER_URL=https://your-codexforall.example.com"
set "API_KEY=YOUR_CODEXFORALL_API_KEY"
set "MODEL=gpt-5.2"

if "!API_KEY!"=="" goto :error_no_key
if "!API_KEY!"=="__API_KEY__" goto :error_no_key

set "codexDir=%USERPROFILE%\.codex"
echo Config dir: !codexDir!
echo.

if exist "!codexDir!" (
    for /f "usebackq delims=" %%a in (`powershell -NoProfile -Command "[DateTime]::Now.ToString('yyyyMMdd_HHmmss')"`) do set TIMESTAMP=%%a

    if not defined TIMESTAMP (
        echo WARN: Failed to get timestamp, skipping backup.
        set "TIMESTAMP=manual"
    )

    if exist "!codexDir!\config.toml" (
        copy "!codexDir!\config.toml" "!codexDir!\config.toml.backup_!TIMESTAMP!" >nul 2>&1
    )

    if exist "!codexDir!\auth.json" (
        copy "!codexDir!\auth.json" "!codexDir!\auth.json.backup_!TIMESTAMP!" >nul 2>&1
    )
) else (
    mkdir "!codexDir!" 2>nul
    if !errorlevel! neq 0 (
        goto :error_mkdir
    )
)

powershell -NoProfile -ExecutionPolicy Bypass -Command "$configPath = Join-Path $env:USERPROFILE '.codex\config.toml'; $dq = [char]34; $providerBlock = @('# BEGIN CODEXFORALL MANAGED PROVIDER','[model_providers.codexforall]',[string]::Concat('name = ', $dq, 'codexforall', $dq),[string]::Concat('base_url = ', $dq, '!CODEXFORALL_SERVER_URL!/v1', $dq),[string]::Concat('wire_api = ', $dq, 'responses', $dq),'# END CODEXFORALL MANAGED PROVIDER') -join [Environment]::NewLine; $existing = ''; if (Test-Path $configPath) { $existing = Get-Content -Raw -Encoding UTF8 $configPath }; $cleaned = $existing.TrimStart([char]0xFEFF); $managedMarker = '# BEGIN CODEXFORALL MANAGED PROVIDER'; $markerIndex = $cleaned.IndexOf($managedMarker, [System.StringComparison]::Ordinal); if ($markerIndex -gt 0) { $prefix = $cleaned.Substring(0, $markerIndex); $suffix = $cleaned.Substring($markerIndex); $prefixLines = @($prefix -split '\r?\n' | Where-Object { $_.Trim() -and -not $_.TrimStart().StartsWith('#') }); if ($prefixLines.Count -gt 0) { $allRepeated = $true; foreach ($line in $prefixLines) { if ($suffix.IndexOf($line, [System.StringComparison]::Ordinal) -lt 0) { $allRepeated = $false; break } }; if ($allRepeated) { $cleaned = $suffix.TrimStart() } } }; $patterns = @('(?ms)^# BEGIN CODEXFORALL MANAGED PROVIDER.*?^# END CODEXFORALL MANAGED PROVIDER\s*','(?ms)^\[model_providers\.codexforall\]\r?\n.*?(?=^\[|\z)'); foreach ($pattern in $patterns) { $cleaned = [regex]::Replace($cleaned, $pattern, '') }; $cleaned = $cleaned.Trim(); $modelProviderLine = [string]::Concat('model_provider = ', $dq, 'codexforall', $dq); $modelLine = [string]::Concat('model = ', $dq, '!MODEL!', $dq); if ([regex]::IsMatch($cleaned, '(?m)^model_provider\s*=.*$')) { $cleaned = [regex]::Replace($cleaned, '(?m)^model_provider\s*=.*$', [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $modelProviderLine }) } elseif ($cleaned.Length -gt 0) { $cleaned = $modelProviderLine + [Environment]::NewLine + [Environment]::NewLine + $cleaned } else { $cleaned = $modelProviderLine }; if ([regex]::IsMatch($cleaned, '(?m)^model\s*=.*$')) { $cleaned = [regex]::Replace($cleaned, '(?m)^model\s*=.*$', [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $modelLine }) } elseif ($cleaned.Length -gt 0) { $cleaned = $modelLine + [Environment]::NewLine + $cleaned } else { $cleaned = $modelLine }; $parts = @($cleaned.Trim(), $providerBlock.Trim()) | Where-Object { $_ }; $output = $parts -join ([Environment]::NewLine + [Environment]::NewLine); $encoding = New-Object System.Text.UTF8Encoding($false); [System.IO.File]::WriteAllText($configPath, $output + [Environment]::NewLine, $encoding)"

if !errorlevel! neq 0 (
    goto :error_write_config
)

if exist "!codexDir!\auth.json" del /f /q "!codexDir!\auth.json" >nul 2>&1

(
echo {
echo   "OPENAI_API_KEY": "!API_KEY!"
echo }
) > "!codexDir!\auth.json" 2>nul

if !errorlevel! neq 0 (
    goto :error_write_auth
)

attrib +h "!codexDir!\auth.json" >nul 2>&1

echo Completed. Files:
echo   - config.toml: !codexDir!\config.toml
echo   - auth.json:  !codexDir!\auth.json

exit /b 0

:error_no_key
echo ERROR: API Key not set.
exit /b 1

:error_mkdir
echo ERROR: Cannot create directory !codexDir!
exit /b 1

:error_write_config
echo ERROR: Cannot write config.toml
exit /b 1

:error_write_auth
echo ERROR: Cannot rewrite auth.json
exit /b 1
