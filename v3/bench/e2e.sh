#!/usr/bin/env bash
# End-to-end benchmark stack on a private Docker network (v3e2e):
# PostgreSQL, Redis (noeviction + AOF), mock upstream, worker, gateway.
#
#   bench/e2e.sh up [users]    build linux binaries, start everything, seed
#   bench/e2e.sh run [label]   one warm k6 pass with gateway stage percentiles
#   bench/e2e.sh down          stop containers, remove network and binaries
#
# The gateway runs with 4 CPUs / 8 GB to match acceptance #2-3.
set -euo pipefail
cd "$(dirname "$0")/.."
export MSYS_NO_PATHCONV=1 GOPROXY=off GOSUMDB=off GOFLAGS='-mod=mod -p=1'
export GOMAXPROCS="${GOMAXPROCS:-2}"
B="$(pwd -W 2>/dev/null || pwd)/bench"
BIN=bench/.bin
NET=v3e2e

env_args() {
  echo "-e V3_PG_DSN=postgres://postgres:t@$NET-pg:5432/t?sslmode=disable -e V3_REDIS_ADDR=$NET-redis:6379 -e V3_SECRET_KEY=$(cat $BIN/secret)"
}

# run_bin NAME BINARY [docker args...] -- [program args...]
run_bin() {
  local name=$1 bin=$2; shift 2
  local dargs=() pargs=()
  while [ $# -gt 0 ] && [ "$1" != "--" ]; do dargs+=("$1"); shift; done
  [ $# -gt 0 ] && shift
  pargs=("$@")
  # shellcheck disable=SC2046
  docker run -d --rm --name "$name" --network $NET -v "$B:/bench:ro" $(env_args) "${dargs[@]}" \
    --entrypoint "/bench/.bin/$bin" redis:7-alpine "${pargs[@]}" >/dev/null
}

up() {
  local users=${1:-500}
  mkdir -p $BIN
  for c in cmd/gateway cmd/worker bench/seed bench/mockupstream/cmd; do
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BIN/$(echo $c | tr / -)" "./$c"
  done
  head -c 32 /dev/urandom | base64 -w0 >$BIN/secret
  docker network create $NET >/dev/null
  docker run -d --rm --name $NET-pg --network $NET -e POSTGRES_PASSWORD=t -e POSTGRES_DB=t postgres:15-alpine -c max_connections=200 >/dev/null
  docker run -d --rm --name $NET-redis --network $NET redis:7-alpine redis-server --maxmemory-policy noeviction --appendonly yes >/dev/null
  run_bin $NET-mock bench-mockupstream-cmd -- -addr 0.0.0.0:18080
  until docker exec $NET-pg pg_isready -U postgres -d t >/dev/null 2>&1; do sleep 1; done
  sleep 2
  # shellcheck disable=SC2046
  docker run --rm --network $NET -v "$B:/bench" $(env_args) --entrypoint /bench/.bin/bench-seed redis:7-alpine \
    -reset -upstream "http://$NET-mock:18080/m/complete/c20/i50/t50" -users "$users" -keys /bench/.bin/keys.json
  run_bin $NET-worker cmd-worker -e V3_WORKER_METRICS_ADDR=0.0.0.0:9102 -e V3_RECONCILE_EVERY=${RECONCILE_EVERY:-5m} -e V3_INTERNAL_GATEWAY_URL="http://$NET-gateway:3000"
  sleep 2
  run_bin $NET-gateway cmd-gateway --cpus 4 --memory 8g -- -addr 0.0.0.0:3000
  sleep 3
  docker run --rm --network $NET curlimages/curl:latest -sf "http://$NET-gateway:3000/readyz" >/dev/null
  echo "e2e stack up: $users users, gateway ready"
}

down() {
  docker stop $NET-gateway $NET-worker $NET-mock $NET-redis $NET-pg >/dev/null 2>&1 || true
  docker network rm $NET >/dev/null 2>&1 || true
  rm -f "$BIN/cmd-gateway" "$BIN/cmd-worker" "$BIN/bench-seed" "$BIN/bench-mockupstream-cmd" "$BIN/secret" "$BIN/keys.json"
  if [ -d "$BIN" ]; then rmdir "$BIN"; fi
}

case "${1:-}" in
  up) up "${2:-500}" ;;
  run) bash bench/e2e_compare.sh "${2:-e2e}" "${DURATION:-45s}" ;;
  down) down ;;
  *) echo "usage: $0 up [users] | run [label] | down" >&2; exit 2 ;;
esac
