#!/usr/bin/env bash
set -euo pipefail

repository="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
acceptance_root="$(mktemp -d)"
override_file="${acceptance_root}/compose.acceptance.yaml"
project_name="gohermit-v07-acceptance"

cleanup() {
  docker compose --project-name "${project_name}" -f "${repository}/compose.yaml" -f "${override_file}" down --remove-orphans >/dev/null 2>&1 || true
  rm -rf "${acceptance_root}"
}
trap cleanup EXIT

mkdir -p "${acceptance_root}/data" "${acceptance_root}/workspace"
GOHERMIT_EVAL_DATA_ROOT="${acceptance_root}" go test ./internal/evals -run '^TestDockerPersistenceFixture$' -count=1

cat >"${override_file}" <<EOF
services:
  gohermit-web:
    user: "$(id -u):$(id -g)"
    volumes:
      - "${acceptance_root}/workspace:/workspace"
      - "${repository}/configs/codex.toml:/config/hermit.toml:ro"
      - "${acceptance_root}/codex:/codex:ro"
      - "${acceptance_root}/skills:/skills:ro"
      - "${acceptance_root}/data:/data"
EOF
mkdir -p "${acceptance_root}/codex" "${acceptance_root}/skills"

compose=(docker compose --project-name "${project_name}" -f "${repository}/compose.yaml" -f "${override_file}")
"${compose[@]}" config >"${acceptance_root}/compose.rendered.yaml"
grep -q '127.0.0.1' "${acceptance_root}/compose.rendered.yaml"
"${compose[@]}" build
"${compose[@]}" up -d

wait_for_workbench() {
  local attempts=0
  until curl --silent --show-error --fail http://127.0.0.1:8787/api/health >"${acceptance_root}/health.json"; do
    attempts=$((attempts + 1))
    if (( attempts >= 60 )); then
      "${compose[@]}" logs
      return 1
    fi
    sleep 1
  done
  curl --silent --show-error --fail http://127.0.0.1:8787/api/info >"${acceptance_root}/info.json"
  grep -q '"version":"0.7.0-dev"' "${acceptance_root}/info.json"
}
wait_for_workbench

manifest() {
  local root="$1"
  find "${root}" -type f -print0 | sort -z | xargs -0 sha256sum
}

test -s "${acceptance_root}/data/employees/index.json"
test -s "${acceptance_root}/data/loops/definitions.json"
test -n "$(find "${acceptance_root}/data/employees/employee-docker/tasks" -type f -name '*.json' -print -quit)"
test -s "${acceptance_root}/data/employees/employee-docker/knowledge/sources.json"
test -s "${acceptance_root}/data/employees/employee-docker/memory/facts.json"
test -n "$(find "${acceptance_root}/workspace/.gohermit/sessions" -type f -name 'session.json' -print -quit)"
manifest "${acceptance_root}/data" >"${acceptance_root}/data.before"
manifest "${acceptance_root}/workspace" >"${acceptance_root}/workspace.before"

"${compose[@]}" down --remove-orphans
"${compose[@]}" build --no-cache
"${compose[@]}" up -d
wait_for_workbench
manifest "${acceptance_root}/data" >"${acceptance_root}/data.after"
manifest "${acceptance_root}/workspace" >"${acceptance_root}/workspace.after"
diff -u "${acceptance_root}/data.before" "${acceptance_root}/data.after"
diff -u "${acceptance_root}/workspace.before" "${acceptance_root}/workspace.after"

if grep -RIE 'api[_-]?key[[:space:]]*[:=][[:space:]]*[^[:space:]"]+|authorization:[[:space:]]*bearer|-----BEGIN .*PRIVATE KEY-----' \
  "${acceptance_root}/data" "${acceptance_root}/workspace"; then
  echo "credential-shaped content found in persistent acceptance data" >&2
  exit 1
fi

"${compose[@]}" ps --status running
