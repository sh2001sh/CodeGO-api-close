#!/usr/bin/env bash
# Local verification for v3. Tools run in Docker; Go modules come from the
# host module cache, so no access to proxy.golang.org is needed.
#
#   scripts/verify.sh              run every step
#   scripts/verify.sh lint race    run selected steps
#
# Steps: boundaries test lint race integration integrationlist pgtest atlas bench
# V3_INTEGRATION_SHARD=commerce|ledger|market|core isolates package groups.
set -euo pipefail

cd "$(dirname "$0")/.."
export GOPROXY=off GOSUMDB=off GOFLAGS='-mod=mod -p=1' MSYS_NO_PATHCONV=1
export GOMAXPROCS="${GOMAXPROCS:-2}"

SRC="$(pwd -W 2>/dev/null || pwd)"
MODCACHE="$(go env GOMODCACHE)"
BUILD_CACHE="$(go env GOCACHE)"
LINT_CACHE="${V3_LINT_CACHE:-$BUILD_CACHE/codego-lint}"
mkdir -p "$BUILD_CACHE" "$LINT_CACHE"
LINT_IMG=golangci/golangci-lint:v2.14.0
ATLAS_IMG=arigaio/atlas:latest
ATLAS_LINT_IMG=arigaio/atlas:0.37.0 # v0.38+ needs an Atlas login for migrate lint
PG_PASS=v3test

# Everything a step starts is registered here and removed on any exit,
# including failures under set -e.
CONTAINERS=()
NETWORKS=()
cleanup() {
  for c in "${CONTAINERS[@]:-}"; do [ -n "$c" ] && docker stop "$c" >/dev/null 2>&1 || true; done
  for n in "${NETWORKS[@]:-}"; do [ -n "$n" ] && docker network rm "$n" >/dev/null 2>&1 || true; done
  rm -f bench/.mockupstream-linux
}
trap cleanup EXIT

go_in_docker() {
  docker run --rm -v "$SRC:/src/v3" -v "$MODCACHE:/go/pkg/mod:ro" -w /src/v3 \
    -v "$BUILD_CACHE:/cache/go-build" -v "$LINT_CACHE:/cache/lint" \
    -e GOPROXY=off -e GOSUMDB=off -e 'GOFLAGS=-mod=mod -p=1' -e GOTOOLCHAIN=local -e "GOMAXPROCS=$GOMAXPROCS" \
    -e GOCACHE=/cache/go-build -e GOLANGCI_LINT_CACHE=/cache/lint "$LINT_IMG" "$@"
}

# start_pg NAME PORT VERSION: disposable PostgreSQL, removed on stop.
start_pg() {
  CONTAINERS+=("$1")
  docker run -d --rm --name "$1" -e POSTGRES_PASSWORD="$PG_PASS" -e POSTGRES_DB=v3test \
    -p "127.0.0.1:$2:5432" "postgres:$3-alpine" >/dev/null
  until docker exec "$1" pg_isready -U postgres -d v3test >/dev/null 2>&1; do sleep 1; done
  sleep 2 # the entrypoint restarts the server once after init
}

step_test() {
  go vet ./...
  go test -count=1 ./...
}

# A nested Go module may still import its parent's internal packages. Check
# the actual dependency graph rather than relying on module separation.
step_boundaries() {
  local imports
  imports=$(go list -deps ./... | grep '^github.com/sh2001sh/new-api/' | grep -v '^github.com/sh2001sh/new-api/v3/' || true)
  if [ -n "$imports" ]; then
    printf 'v3 depends on v2 packages:\n%s\n' "$imports" >&2
    return 1
  fi
}

# Integration tests live behind the pgintegration tag, so lint both builds.
step_lint() {
  go_in_docker sh -c 'golangci-lint run --concurrency=2 ./... && golangci-lint run --concurrency=2 --build-tags=pgintegration ./...'
}

step_race() { go_in_docker go test -race -count=1 ./...; }

integration_packages() {
  local shard="${V3_INTEGRATION_SHARD:-all}" packages package group
  case "$shard" in all|commerce|ledger|market|core) ;; *) echo "invalid integration shard: $shard" >&2; return 2 ;; esac
  packages=$(go list -tags=pgintegration ./...)
  while IFS= read -r package; do
    case "$package" in
      */internal/commerce|*/internal/incentives) group=commerce ;;
      */internal/billing|*/internal/billing/*|*/internal/catalog|*/internal/catalogcontrol) group=ledger ;;
      */internal/marketplace|*/internal/channelmarket) group=market ;;
      *) group=core ;;
    esac
    if [ "$shard" = all ] || [ "$shard" = "$group" ]; then printf '%s\n' "$package"; fi
  done <<< "$packages"
}

step_integrationlist() { integration_packages; }

# Integration suites may drop schemas and flush Redis. Both services are
# disposable and inaccessible from the host or production networks.
step_integration() {
  local net="v3-verify-$$" pg="v3-verify-pg-$$" rd="v3-verify-redis-$$" ready=0
  local selected
  selected=$(integration_packages)
  [ -n "$selected" ] || { echo 'integration shard has no packages' >&2; return 1; }
  local -a packages
  mapfile -t packages <<< "$selected"
  docker network create "$net" >/dev/null
  NETWORKS+=("$net")
  docker run -d --rm --name "$pg" --network "$net" \
    -e POSTGRES_PASSWORD="$PG_PASS" -e POSTGRES_DB=v3test postgres:15-alpine postgres -p 55497 >/dev/null
  CONTAINERS+=("$pg")
  docker run -d --rm --name "$rd" --network "$net" redis:7-alpine \
    redis-server --maxmemory-policy noeviction --appendonly yes >/dev/null
  CONTAINERS+=("$rd")
  for ((i=0; i<60; i++)); do
    if docker exec "$pg" pg_isready -p 55497 -U postgres -d v3test >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
  done
  [ "$ready" = 1 ] || { echo 'integration PostgreSQL did not become ready' >&2; return 1; }
  sleep 2
  # Dedicated suites refuse arbitrary storage, and some require loopback or
  # port 55497. Share this disposable PG's network namespace to satisfy their
  # safety fences without publishing ports or weakening the test guards.
  for database in notifications_test adminops_tests audit_sampler_test community_sync_test codego_policy_verify; do
    docker exec "$pg" createdb -p 55497 -U postgres "$database"
  done
  docker run --rm --network "container:$pg" -v "$SRC:/src/v3" -v "$MODCACHE:/go/pkg/mod:ro" -w /src/v3 \
    -v "$BUILD_CACHE:/cache/go-build" -e GOCACHE=/cache/go-build \
    -e GOPROXY=off -e GOSUMDB=off -e 'GOFLAGS=-mod=mod -p=1' -e GOTOOLCHAIN=local -e "GOMAXPROCS=$GOMAXPROCS" \
    -e "V3_TEST_PG_DSN=postgres://postgres:$PG_PASS@127.0.0.1:55497/v3test?sslmode=disable" \
    -e "V3_NOTIFICATIONS_TEST_PG_DSN=postgres://postgres:$PG_PASS@127.0.0.1:55497/notifications_test?sslmode=disable" \
    -e "V3_ADMINOPS_TEST_PG_DSN=postgres://postgres:$PG_PASS@127.0.0.1:55497/adminops_tests?sslmode=disable" \
    -e "V3_AUDIT_TEST_PG_DSN=postgres://postgres:$PG_PASS@127.0.0.1:55497/audit_sampler_test?sslmode=disable" \
    -e "CODEGO_RATING_RELAY_TEST_DSN=postgres://postgres:$PG_PASS@127.0.0.1:55497/community_sync_test?sslmode=disable" \
    -e "V3_POLICY_TEST_PG_DSN=postgres://postgres:$PG_PASS@127.0.0.1:55497/codego_policy_verify?sslmode=disable" \
    -e "V3_MIGRATION_TEST_PG_DSN=postgres://postgres:$PG_PASS@127.0.0.1:55497/v3test?sslmode=disable" \
    -e "V3_MIGRATION_TEST_REDIS_ADDR=$rd:6379" \
    -e "V3_TEST_REDIS_ADDR=$rd:6379" \
    -e "CODEGO_TEST_REDIS_ADDR=$rd:6379" \
    -e "V3_TEST_COMMERCE_REDIS_ADDR=$rd:6379" "$LINT_IMG" \
    go test -race -tags=pgintegration -count=1 -p 1 -timeout=10m "${packages[@]}"
}

step_pgtest() {
  local v name port
  for v in 15 17; do
    name="v3-pgtest-$v"; port=$((55400 + v))
    start_pg "$name" "$port" "$v"
    V3_TEST_PG_DSN="postgres://postgres:$PG_PASS@127.0.0.1:$port/v3test?sslmode=disable" \
      go test -tags=pgintegration -count=1 ./migrations/
    docker stop "$name" >/dev/null 2>&1 || true
  done
}

step_atlas() {
  local net="v3-atlas-$$" pg="v3-atlasdev-$$" ready=0
  docker network create "$net" >/dev/null
  NETWORKS+=("$net")
  docker run -d --rm --name "$pg" --network "$net" \
    -e POSTGRES_PASSWORD="$PG_PASS" -e POSTGRES_DB=v3test postgres:15-alpine >/dev/null
  CONTAINERS+=("$pg")
  for ((i=0; i<60; i++)); do
    if docker exec "$pg" pg_isready -U postgres -d v3test >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
  done
  [ "$ready" = 1 ] || { echo 'Atlas PostgreSQL did not become ready' >&2; return 1; }
  sleep 2
  local dev="postgres://postgres:$PG_PASS@$pg:5432/v3test?sslmode=disable"
  local mount="$SRC/migrations:/migrations"
  docker run --rm --network "$net" -v "$mount:ro" "$ATLAS_IMG" migrate validate --dir file:///migrations --dev-url "$dev"
  docker run --rm --network "$net" -v "$mount" "$ATLAS_LINT_IMG" migrate lint --dir file:///migrations \
    --dev-url "$dev" --latest 100
  if ! git diff --quiet -- migrations/atlas.sum 2>/dev/null; then
    echo "note: atlas.sum changed; commit it with the migration" >&2
  fi
}

step_bench() {
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bench/.mockupstream-linux ./bench/mockupstream/cmd
  NETWORKS+=(v3bench)
  CONTAINERS+=(v3-mock)
  docker network create v3bench >/dev/null
  docker run -d --rm --name v3-mock --network v3bench -v "$SRC/bench:/bench:ro" \
    --entrypoint /bench/.mockupstream-linux redis:7-alpine -addr 0.0.0.0:18080 >/dev/null
  sleep 2
  docker run --rm --network v3bench -v "$SRC/bench:/bench:ro" -e TARGET=http://v3-mock:18080 \
    -e STREAMS="${STREAMS:-300}" -e RPS="${RPS:-300}" -e DURATION="${DURATION:-30s}" \
    grafana/k6:latest run --quiet /bench/gateway_stream.js
}

steps=("$@")
[ ${#steps[@]} -gt 0 ] || steps=(boundaries test lint race integration pgtest atlas bench)
for s in "${steps[@]}"; do
  echo "==> $s"
  "step_$s"
done
echo "==> all steps passed: ${steps[*]}"
