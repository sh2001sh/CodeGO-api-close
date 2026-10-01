#!/usr/bin/env bash
# Runs one warm k6 pass against the v3e2e gateway and prints steady-state
# histogram deltas plus Redis pool wait deltas. Assumes the v3e2e stack from
# the M1 benchmark is up. Usage: bench/e2e_compare.sh <label> [duration]
set -euo pipefail
cd "$(dirname "$0")/.."
export MSYS_NO_PATHCONV=1
B="$(pwd -W 2>/dev/null || pwd)/bench"
label=$1 duration=${2:-30s}
scrape() { docker run --rm --network v3e2e curlimages/curl:latest -s http://v3e2e-gateway:3000/metrics; }
k6() {
  docker run --rm --user root --network v3e2e -v "$B:/bench:ro" -e TARGET=http://v3e2e-gateway:3000 \
    -e KEYS_FILE=/bench/.bin/keys.json -e STREAMS="${STREAMS:-2000}" -e RPS="${RPS:-1000}" -e DURATION="$1" grafana/k6:latest run --quiet /bench/gateway_stream.js
}
metric() { grep -E "^$2 " "$1" | awk '{print $2}'; }
pct() { # pct <before> <after> <bucket selector>
  join <(grep -F "$3" "$1" | sed -E 's/.*le="([^"]+)"\} ([0-9e.+]+)/\1 \2/' | sort) \
       <(grep -F "$3" "$2" | sed -E 's/.*le="([^"]+)"\} ([0-9e.+]+)/\1 \2/' | sort) |
    awk '{printf "%s %.0f\n", $1, $3-$2}' | sort -g |
    awk '{le[NR]=$1; c[NR]=$2; n=NR} END {split("0.5 0.9 0.99 0.999", q, " "); s=""; for (i=1;i<=4;i++){t=q[i]*c[n]; for (j=1;j<=n;j++) if (c[j]>=t) {s=s sprintf(" p%s<=%s", q[i]*100, le[j]); break}} print s}'
}

k6 15s >/dev/null 2>&1 || echo 'warm-up failed its thresholds; the measured pass still must succeed' >&2
before=$(mktemp) after=$(mktemp) result=$(mktemp)
trap 'rm -f "$before" "$after" "$result"' EXIT
scrape >"$before"
# Preserve k6's failure after printing the diagnostic metrics.
status=0
k6 "$duration" >"$result" 2>&1 || status=$?
cat "$result"
scrape >"$after"
echo "[$label]"
printf '  overhead:%s\n' "$(pct "$before" "$after" codego_gateway_overhead_seconds_bucket)"
printf '  reserve: %s\n' "$(pct "$before" "$after" 'stage_seconds_bucket{stage="reserve",')"
printf '  finalize:%s\n' "$(pct "$before" "$after" 'stage_seconds_bucket{stage="finalize",')"
printf '  authorize:%s\n' "$(pct "$before" "$after" 'stage_seconds_bucket{stage="authorize",')"
w0=$(metric "$before" codego_redis_pool_waits_total) w1=$(metric "$after" codego_redis_pool_waits_total)
s0=$(metric "$before" codego_redis_pool_wait_seconds_total) s1=$(metric "$after" codego_redis_pool_wait_seconds_total)
awk -v w="$((${w1%.*} - ${w0%.*}))" -v s0="$s0" -v s1="$s1" 'BEGIN {printf "  redis pool waits: %d, total %.2fs\n", w, s1-s0}'
exit "$status"
