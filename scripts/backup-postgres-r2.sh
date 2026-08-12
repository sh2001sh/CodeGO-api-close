#!/usr/bin/env bash
set -Eeuo pipefail

# Daily encrypted PostgreSQL backup to Cloudflare R2 (S3-compatible).
# Required: SQL_DSN, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET,
# R2_ENDPOINT, and AGE_RECIPIENT. AWS_DEFAULT_REGION may remain "auto".

umask 077

: "${SQL_DSN:?SQL_DSN is required}"
: "${R2_ACCESS_KEY_ID:?R2_ACCESS_KEY_ID is required}"
: "${R2_SECRET_ACCESS_KEY:?R2_SECRET_ACCESS_KEY is required}"
: "${R2_BUCKET:?R2_BUCKET is required}"
: "${R2_ENDPOINT:?R2_ENDPOINT is required}"
: "${AGE_RECIPIENT:?AGE_RECIPIENT is required}"

BACKUP_PREFIX="${BACKUP_PREFIX:-codego/postgres}"
RETENTION_DAYS="${RETENTION_DAYS:-35}"
TMP_DIR="${BACKUP_TMP_DIR:-/mnt/codego-data/postgres-backups/tmp}"
AWS_REGION="${AWS_DEFAULT_REGION:-auto}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BASE_NAME="${BACKUP_PREFIX%/}/codego-${STAMP}.dump.age"

command -v pg_dump >/dev/null || { echo "pg_dump is required" >&2; exit 1; }
command -v age >/dev/null || { echo "age is required" >&2; exit 1; }
command -v aws >/dev/null || { echo "aws CLI is required" >&2; exit 1; }

mkdir -p -- "$TMP_DIR"
if command -v flock >/dev/null; then
  exec 9>"${TMP_DIR%/}/.backup.lock"
  flock -n 9 || { echo "another backup is already running" >&2; exit 0; }
fi
WORK_DIR="$(mktemp -d "${TMP_DIR%/}/run.XXXXXX")"
RAW_DUMP="${WORK_DIR}/codego-${STAMP}.dump"
ENCRYPTED_DUMP="${WORK_DIR}/codego-${STAMP}.dump.age"

cleanup() { rm -rf -- "$WORK_DIR"; }
trap cleanup EXIT

pg_dump --format=custom --no-owner --no-acl "$SQL_DSN" > "$RAW_DUMP"
test -s "$RAW_DUMP"
age --recipient "$AGE_RECIPIENT" --output "$ENCRYPTED_DUMP" "$RAW_DUMP"
test -s "$ENCRYPTED_DUMP"

export AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID"
export AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY"
export AWS_DEFAULT_REGION="$AWS_REGION"

aws --endpoint-url "$R2_ENDPOINT" s3 cp "$ENCRYPTED_DUMP" "s3://${R2_BUCKET}/${BASE_NAME}" --only-show-errors
CHECKSUM="$(sha256sum "$ENCRYPTED_DUMP" | cut -d' ' -f1)"
printf '%s  %s\n' "$CHECKSUM" "$BASE_NAME" > "$WORK_DIR/checksum.txt"
aws --endpoint-url "$R2_ENDPOINT" s3 cp "$WORK_DIR/checksum.txt" "s3://${R2_BUCKET}/${BASE_NAME}.sha256" --only-show-errors

# Keep the latest N daily files. Deletion is restricted to this backup prefix.
KEEP="${RETENTION_DAYS}"
mapfile -t OBJECTS < <(aws --endpoint-url "$R2_ENDPOINT" s3api list-objects-v2 \
  --bucket "$R2_BUCKET" --prefix "${BACKUP_PREFIX%/}/codego-" \
  --query 'Contents[?ends_with(Key, `.dump.age`)].Key' --output text | tr '\t' '\n' | sort -r)
if (( ${#OBJECTS[@]} > KEEP )); then
  for key in "${OBJECTS[@]:KEEP}"; do
    aws --endpoint-url "$R2_ENDPOINT" s3 rm "s3://${R2_BUCKET}/${key}" --only-show-errors
    aws --endpoint-url "$R2_ENDPOINT" s3 rm "s3://${R2_BUCKET}/${key}.sha256" --only-show-errors || true
  done
fi

echo "backup uploaded: s3://${R2_BUCKET}/${BASE_NAME}"
