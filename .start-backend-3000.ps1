$env:BROWSER = 'none'
Remove-Item Env:SQL_DSN -ErrorAction SilentlyContinue
Remove-Item Env:SQLITE_PATH -ErrorAction SilentlyContinue
Remove-Item Env:GOPROXY -ErrorAction SilentlyContinue
Remove-Item Env:GOSUMDB -ErrorAction SilentlyContinue
Remove-Item Env:GOMODCACHE -ErrorAction SilentlyContinue
Remove-Item Env:GOCACHE -ErrorAction SilentlyContinue
Remove-Item Env:GOTMPDIR -ErrorAction SilentlyContinue
Set-Location 'E:\sh\Coding\cpa_bussiness\new-api'
$env:GOPROXY = 'https://goproxy.cn,direct'
$env:GOSUMDB = 'sum.golang.org'
$env:GOMODCACHE = 'E:\sh\Coding\cpa_bussiness\new-api\.tmp\gomodcache'
$env:GOCACHE = 'E:\sh\Coding\cpa_bussiness\new-api\.tmp\gocache'
$env:GOTMPDIR = 'E:\sh\Coding\cpa_bussiness\new-api\.tmp\gotmp'
$env:SQLITE_PATH = 'E:\sh\Coding\cpa_bussiness\new-api\one-api.db?_busy_timeout=30000'
$env:SESSION_STORE = 'cookie'
$env:SESSION_SECRET = 'local-development-session-secret-3000'

New-Item -ItemType Directory -Force -Path $env:GOMODCACHE | Out-Null
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null

go run ./cmd/db-migrate *>> '.backend-live.out.log'
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

& '.\control-api.new.exe' --port 3000 *>> '.backend-live.out.log'
