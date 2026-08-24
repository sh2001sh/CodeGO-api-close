#!/usr/bin/env bash
set -Eeuo pipefail

# Encrypted logical backups for the CodeGo and Temporal PostgreSQL clusters.
# The Temporal PostgreSQL container must expose its local socket on PGPORT 5433.

umask 077

BACKUP_PREFIX="${BACKUP_PREFIX:-codego/postgres-v2}"
RETENTION_DAYS="${RETENTION_DAYS:-35}"
BACKUP_TMP_DIR="${BACKUP_TMP_DIR:-/var/lib/codego-backup/tmp}"
R2_BUCKET="${R2_BUCKET:-codego}"
AWS_PROFILE="${AWS_PROFILE:-codego-r2}"
R2_ENDPOINT="${R2_ENDPOINT:-$(cat /etc/codego/r2-endpoint)}"
AGE_RECIPIENT_FILE="${AGE_RECIPIENT_FILE:-/etc/codego/backup-age-recipient}"
TEMPORAL_CONTAINER="${TEMPORAL_CONTAINER:-codego-temporal-postgres}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

mkdir -p -- "$BACKUP_TMP_DIR"
exec 9>"${BACKUP_TMP_DIR%/}/.backup.lock"
flock -n 9 || { echo "another backup is already running" >&2; exit 0; }
WORK_DIR="$(mktemp -d "${BACKUP_TMP_DIR%/}/run.XXXXXX")"
trap 'rm -rf -- "$WORK_DIR"' EXIT

AGE_RECIPIENT="$(head -n1 "$AGE_RECIPIENT_FILE")"
test -n "$R2_ENDPOINT"
test -n "$AGE_RECIPIENT"
command -v age >/dev/null
command -v aws >/dev/null
command -v pg_dump >/dev/null
command -v pg_dumpall >/dev/null

dump_and_upload() {
  local name="$1" extension="$2"; shift 2
  local raw="$WORK_DIR/${name}.${extension}"
  local encrypted="$WORK_DIR/${name}.${extension}.age"
  local object="${BACKUP_PREFIX%/}/${STAMP}/${name}.${extension}.age"

  "$@" > "$raw"
  test -s "$raw"
  age --recipient "$AGE_RECIPIENT" --output "$encrypted" "$raw"
  test -s "$encrypted"

  AWS_PROFILE="$AWS_PROFILE" aws --endpoint-url "$R2_ENDPOINT" s3 cp \
    "$encrypted" "s3://${R2_BUCKET}/${object}" --only-show-errors
  sha256sum "$encrypted" | awk -v object="$object" '{print $1 "  " object}' \
    > "$WORK_DIR/${name}.sha256"
  AWS_PROFILE="$AWS_PROFILE" aws --endpoint-url "$R2_ENDPOINT" s3 cp \
    "$WORK_DIR/${name}.sha256" "s3://${R2_BUCKET}/${object}.sha256" --only-show-errors

  printf '%s|%s|%s\n' "$name" "$object" "$(stat -c '%s' "$encrypted")"
}

dump_and_upload main-neondb dump sudo -u postgres pg_dump --format=custom --dbname=neondb
dump_and_upload main-roles sql sudo -u postgres pg_dumpall --globals-only
dump_and_upload temporal dump sudo docker exec -e PGPORT=5433 "$TEMPORAL_CONTAINER" \
  pg_dump -U temporal --format=custom --dbname=temporal
dump_and_upload temporal-visibility dump sudo docker exec -e PGPORT=5433 "$TEMPORAL_CONTAINER" \
  pg_dump -U temporal --format=custom --dbname=temporal_visibility
dump_and_upload temporal-roles sql sudo docker exec -e PGPORT=5433 "$TEMPORAL_CONTAINER" \
  pg_dumpall -U temporal --globals-only

mapfile -t generations < <(
  AWS_PROFILE="$AWS_PROFILE" aws --endpoint-url "$R2_ENDPOINT" \
    s3 ls "s3://${R2_BUCKET}/${BACKUP_PREFIX%/}/" |
    awk '$1 == "PRE" {gsub("/", "", $2); print $2}' | sort -r
)
if (( ${#generations[@]} > RETENTION_DAYS )); then
  for generation in "${generations[@]:RETENTION_DAYS}"; do
    AWS_PROFILE="$AWS_PROFILE" aws --endpoint-url "$R2_ENDPOINT" s3 rm \
      "s3://${R2_BUCKET}/${BACKUP_PREFIX%/}/${generation}/" --recursive --only-show-errors
  done
fi

echo "backup uploaded: ${STAMP}"
