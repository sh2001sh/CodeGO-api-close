#!/usr/bin/env bash
set -euo pipefail

SERVER_URL="https://your-codexforall.example.com"
API_BASE="${SERVER_URL%/}/v1"
API_KEY="YOUR_CODEXFORALL_API_KEY"
MODEL="gpt-5.2"

TARGET_DIR="${HOME}/.config/codexforall"
TARGET_FILE="${TARGET_DIR}/codex-env.sh"
CODEX_DIR="${HOME}/.codex"
CODEX_CONFIG_FILE="${CODEX_DIR}/config.toml"
CODEX_AUTH_FILE="${CODEX_DIR}/auth.json"
PROFILE_FILE="${HOME}/.bashrc"
SOURCE_LINE='[ -f "$HOME/.config/codexforall/codex-env.sh" ] && source "$HOME/.config/codexforall/codex-env.sh"'

if [[ "${SHELL:-}" == */zsh ]]; then
  PROFILE_FILE="${HOME}/.zshrc"
fi

mkdir -p "${TARGET_DIR}"
mkdir -p "${CODEX_DIR}"

PYTHON_BIN="python3"
if ! command -v "${PYTHON_BIN}" >/dev/null 2>&1; then
  PYTHON_BIN="python"
fi

if ! command -v "${PYTHON_BIN}" >/dev/null 2>&1; then
  printf 'python3/python is required to update %s\n' "${CODEX_CONFIG_FILE}" >&2
  exit 1
fi

cat > "${TARGET_FILE}" <<EOF
export OPENAI_BASE_URL="${API_BASE}"
export OPENAI_API_BASE="${API_BASE}"
export OPENAI_MODEL="${MODEL}"
EOF

"${PYTHON_BIN}" - "${CODEX_CONFIG_FILE}" "${API_BASE}" "${MODEL}" <<'PY'
from pathlib import Path
import re
import sys

config_path = Path(sys.argv[1])
api_base = sys.argv[2]
model_name = sys.argv[3]
provider_name = "codexforall"
provider_block = """
# BEGIN CODEXFORALL MANAGED PROVIDER
[model_providers.codexforall]
name = "codexforall"
base_url = "{api_base}"
wire_api = "responses"
# END CODEXFORALL MANAGED PROVIDER
""".format(api_base=api_base).strip()

existing = config_path.read_text(encoding="utf-8") if config_path.exists() else ""
existing = existing.lstrip("\ufeff")
cleaned = existing
managed_marker = "# BEGIN CODEXFORALL MANAGED PROVIDER"
marker_index = cleaned.find(managed_marker)
if marker_index > 0:
    prefix = cleaned[:marker_index]
    suffix = cleaned[marker_index:]
    prefix_lines = [
        line.strip()
        for line in prefix.splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]
    if prefix_lines and all(line in suffix for line in prefix_lines):
        cleaned = suffix.lstrip()
patterns = [
    r"(?ms)^# BEGIN CODEXFORALL MANAGED PROVIDER.*?^# END CODEXFORALL MANAGED PROVIDER\s*",
    r"(?ms)^\[model_providers\.codexforall\]\r?\n.*?(?=^\[|\Z)",
]
for pattern in patterns:
    cleaned = re.sub(pattern, "", cleaned)

cleaned = cleaned.strip()
root_match = re.search(r"(?m)^\[", cleaned)
if root_match:
    root_section = cleaned[:root_match.start()].strip()
    table_section = cleaned[root_match.start():].strip()
else:
    root_section = cleaned
    table_section = ""

root_lines = [line for line in root_section.splitlines() if line.strip()]

def upsert_root_setting(lines, key, value):
    setting = f'{key} = "{value}"'
    output = []
    replaced = False
    pattern = re.compile(rf"^{re.escape(key)}\s*=")
    for line in lines:
        if pattern.match(line):
            if not replaced:
                output.append(setting)
                replaced = True
            continue
        output.append(line)
    if not replaced:
        output.append(setting)
    return output

root_lines = upsert_root_setting(root_lines, "model_provider", provider_name)
root_lines = upsert_root_setting(root_lines, "model", model_name)

parts = ["\n".join(root_lines).strip(), table_section, provider_block]
output = "\n\n".join(part for part in parts if part.strip()) + "\n"
config_path.write_text(output, encoding="utf-8")
PY

chmod 600 "${TARGET_FILE}"
chmod 600 "${CODEX_CONFIG_FILE}"

: > "${CODEX_AUTH_FILE}"
cat > "${CODEX_AUTH_FILE}" <<EOF
{"OPENAI_API_KEY":"${API_KEY}"}
EOF
chmod 600 "${CODEX_AUTH_FILE}"

touch "${PROFILE_FILE}"

if ! grep -qxF "${SOURCE_LINE}" "${PROFILE_FILE}"; then
  printf '\n%s\n' "${SOURCE_LINE}" >> "${PROFILE_FILE}"
fi

printf 'Configured Codex CLI for codexforall.\n'
printf 'Environment file: %s\n' "${TARGET_FILE}"
printf 'Codex config file: %s\n' "${CODEX_CONFIG_FILE}"
printf 'Codex auth file: %s\n' "${CODEX_AUTH_FILE}"
printf 'Shell profile: %s\n' "${PROFILE_FILE}"
printf 'Run: source "%s"\n' "${PROFILE_FILE}"
printf 'Then start Codex with: codex\n'

if ! command -v codex >/dev/null 2>&1; then
  printf 'Codex CLI not found. Install it with: npm install -g @openai/codex\n'
fi
