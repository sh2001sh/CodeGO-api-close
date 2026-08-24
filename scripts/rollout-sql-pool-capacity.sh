#!/usr/bin/env bash
set -Eeuo pipefail

# Expands PostgreSQL capacity while bounding each CodeGo service. Keep the
# total below PostgreSQL's connection limit so a gateway surge cannot starve
# workers, migrations, Temporal, or administrative access.
backup_dir="${1:?pass a writable backup directory}"
postgres_container="${POSTGRES_CONTAINER:-postgres}"
postgres_max_connections="${POSTGRES_MAX_CONNECTIONS:-300}"
suffix="pre-sql-pool-$(date -u +%Y%m%dT%H%M%SZ)"

services=(
  "new-api-v2-workflow|30|10"
  "new-api-v2-ledger|45|10"
  "new-api-v2-control|45|15"
  "new-api-v2-gateway|120|30"
)
deployed_names=()
old_postgres_max_connections=""
postgres_changed=false

rollback_service() {
  local name="$1"
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker rename "${name}-${suffix}" "$name" >/dev/null 2>&1 || true
  docker start "$name" >/dev/null 2>&1 || true
}

rollback_all() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    if [ "$postgres_changed" = true ] && [ -n "$old_postgres_max_connections" ]; then
      docker exec "$postgres_container" psql -U root -d new-api -c \
        "ALTER SYSTEM SET max_connections = '${old_postgres_max_connections}';" >/dev/null 2>&1 || true
      docker restart "$postgres_container" >/dev/null 2>&1 || true
    fi
    for ((index=${#deployed_names[@]} - 1; index>=0; index--)); do
      rollback_service "${deployed_names[index]}"
    done
  fi
  exit "$status"
}
trap rollback_all EXIT

mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"
old_postgres_max_connections="$(docker exec "$postgres_container" psql -U root -d new-api -At -c 'SHOW max_connections;')"
printf 'old_postgres_max_connections=%s\n' "$old_postgres_max_connections" > "${backup_dir}/postgres-capacity-before.txt"

for service in "${services[@]}"; do
  IFS='|' read -r name max_open max_idle <<< "$service"
  docker inspect "$name" >/dev/null
  docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$name" > "${backup_dir}/${name}.env"
  docker inspect --format '{{.Config.Image}}' "$name" > "${backup_dir}/${name}.image"
  printf 'SQL_MAX_OPEN_CONNS=%s\nSQL_MAX_IDLE_CONNS=%s\nSQL_MAX_LIFETIME=300\n' \
    "$max_open" "$max_idle" >> "${backup_dir}/${name}.env"
  chmod 0600 "${backup_dir}/${name}.env"
done

for service in "${services[@]}"; do
  IFS='|' read -r name _ _ <<< "$service"
  image="$(<"${backup_dir}/${name}.image")"

  docker stop "$name" >/dev/null
  docker rename "$name" "${name}-${suffix}"
  deployed_names+=("$name")

  run_args=(--detach --name "$name" --network host --restart unless-stopped --log-opt max-size=20m --log-opt max-file=5 --env-file "${backup_dir}/${name}.env")
  docker run "${run_args[@]}" "$image" >/dev/null

  for _ in $(seq 1 30); do
    [ "$(docker inspect --format '{{.State.Running}}' "$name")" = true ] && break
    sleep 1
  done
  [ "$(docker inspect --format '{{.State.Running}}' "$name")" = true ]
done

docker exec "$postgres_container" psql -U root -d new-api -c \
  "ALTER SYSTEM SET max_connections = '${postgres_max_connections}';" >/dev/null
postgres_changed=true
docker restart "$postgres_container" >/dev/null
for _ in $(seq 1 30); do
  docker exec "$postgres_container" pg_isready -U root -d new-api >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$postgres_container" pg_isready -U root -d new-api >/dev/null
[ "$(docker exec "$postgres_container" psql -U root -d new-api -At -c 'SHOW max_connections;')" = "$postgres_max_connections" ]

for _ in $(seq 1 60); do
  curl -fsS --max-time 5 http://127.0.0.1:3000/api/status >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS --max-time 5 http://127.0.0.1:3000/api/status >/dev/null
for _ in $(seq 1 60); do
  curl -sSI --max-time 5 http://127.0.0.1:3001/health 2>/dev/null |
    tr -d '\r' |
    grep -q '^X-New-Api-Version:' && break
  sleep 1
done
curl -sSI --max-time 5 http://127.0.0.1:3001/health |
  tr -d '\r' |
  grep -q '^X-New-Api-Version:'

docker exec "$postgres_container" psql -U root -d new-api -At -c \
  "SHOW max_connections; SELECT state || ':' || count(*) FROM pg_stat_activity GROUP BY state ORDER BY state;" \
  > "${backup_dir}/postgres-capacity-after.txt"

trap - EXIT
printf 'SQL_POOL_CAPACITY_APPLIED backup_dir=%s rollback_suffix=%s\n' "$backup_dir" "$suffix"
