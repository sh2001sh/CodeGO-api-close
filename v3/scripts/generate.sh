#!/usr/bin/env bash
# Regenerate the backend contract using pinned tools. Frontend types are
# generated and checked by web/app's generate:api and check:api scripts.
set -euo pipefail
cd "$(dirname "$0")/.."
go generate ./api
