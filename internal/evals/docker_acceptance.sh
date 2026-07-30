#!/usr/bin/env bash
set -euo pipefail

repository="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
project_name="${GOHERMIT_ACCEPTANCE_PROJECT:-gohermit-phase5-acceptance-$$}"
acceptance_port="${GOHERMIT_ACCEPTANCE_PORT:-18787}"
if [[ ! "${project_name}" =~ ^gohermit-phase5-acceptance-[A-Za-z0-9][A-Za-z0-9_-]*$ ]]; then
  echo "GOHERMIT_ACCEPTANCE_PROJECT must be a unique gohermit-phase5-acceptance-* name" >&2
  exit 1
fi
if [[ ! "${acceptance_port}" =~ ^[0-9]+$ ]] || (( acceptance_port < 1024 || acceptance_port > 65535 )); then
  echo "GOHERMIT_ACCEPTANCE_PORT must be an unprivileged TCP port" >&2
  exit 1
fi
if (( acceptance_port == 8787 )); then
  echo "GOHERMIT_ACCEPTANCE_PORT must not target the production port" >&2
  exit 1
fi
export GOHERMIT_WEB_PORT="${acceptance_port}"
export GOHERMIT_WEB_BIND="127.0.0.1"
base_url="http://127.0.0.1:${acceptance_port}"
node_image="${GOHERMIT_ACCEPTANCE_NODE_IMAGE:-node:22-bookworm-slim}"
runtime_image="${GOHERMIT_ACCEPTANCE_RUNTIME_IMAGE:-alpine/git:latest}"
pnpm_registry="${GOHERMIT_ACCEPTANCE_PNPM_REGISTRY:-https://registry.npmjs.org}"
build_audit_image="${project_name}-build-audit:local"
case "${GOHERMIT_ACCEPTANCE_INJECT_FAILURE_AFTER_BUILD_AUDIT:-}" in
  ""|1|TERM) ;;
  *)
    echo "GOHERMIT_ACCEPTANCE_INJECT_FAILURE_AFTER_BUILD_AUDIT must be empty, 1, or TERM" >&2
    exit 1
    ;;
esac
if docker image inspect "${build_audit_image}" >/dev/null 2>&1 ||
  [[ -n "$(docker ps -aq --filter "label=com.docker.compose.project=${project_name}")" ]] ||
  [[ -n "$(docker network ls -q --filter "label=com.docker.compose.project=${project_name}")" ]] ||
  docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -Eq "^${project_name}-"; then
  echo "GOHERMIT_ACCEPTANCE_PROJECT already owns Docker artifacts; choose a unique name" >&2
  exit 1
fi
acceptance_root="$(mktemp -d)"
override_file="${acceptance_root}/compose.acceptance.yaml"

cleanup() {
  local status=$?
  local cleanup_failed=0
  trap - EXIT INT TERM
  set +e
  docker compose --project-name "${project_name}" -f "${repository}/compose.yaml" -f "${override_file}" down --remove-orphans --rmi local >/dev/null 2>&1 ||
    cleanup_failed=1
  if docker image inspect "${build_audit_image}" >/dev/null 2>&1; then
    docker image rm "${build_audit_image}" >/dev/null 2>&1 || cleanup_failed=1
  fi
  if docker image inspect "${build_audit_image}" >/dev/null 2>&1 ||
    [[ -n "$(docker ps -aq --filter "label=com.docker.compose.project=${project_name}")" ]] ||
    [[ -n "$(docker network ls -q --filter "label=com.docker.compose.project=${project_name}")" ]] ||
    docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -Eq "^${project_name}-"; then
    echo "scoped Docker acceptance cleanup left project artifacts" >&2
    cleanup_failed=1
  fi
  if [[ -n "${acceptance_root}" && -d "${acceptance_root}" ]]; then
    rm -rf -- "${acceptance_root}"
  fi
  if (( status == 0 && cleanup_failed != 0 )); then
    status=1
  fi
  exit "${status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "${acceptance_root}/data" "${acceptance_root}/workspace"
GOHERMIT_EVAL_DATA_ROOT="${acceptance_root}" go test ./internal/evals -run '^TestDockerPersistenceFixture$' -count=1

cat >"${override_file}" <<EOF
services:
  gohermit-web:
    build:
      args:
        NODE_IMAGE: "${node_image}"
        PNPM_REGISTRY: "${pnpm_registry}"
        RUNTIME_IMAGE: "${runtime_image}"
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

inspect_build_stage() {
  docker build \
    --target build \
    --tag "${build_audit_image}" \
    --build-arg "GO_IMAGE=${GOHERMIT_GO_IMAGE:-golang:1.26-bookworm}" \
    --build-arg "NODE_IMAGE=${node_image}" \
    --build-arg "PNPM_REGISTRY=${pnpm_registry}" \
    --build-arg "RUNTIME_IMAGE=${runtime_image}" \
    "${repository}"
  docker run --rm --entrypoint /bin/sh "${build_audit_image}" -eu -c '
    for required in cmd internal protocol go.mod go.sum; do
      test -e "/src/${required}"
    done
    if find /src -mindepth 1 \( \
      -type d \( \
        -name .claude -o \
        -name .codegraph -o \
        -name .cursor -o \
        -name .gemini -o \
        -name .gohermit -o \
        -name sandbox \
      \) -o \
      -type f \( \
        -name .mcp.json -o \
        -name '.env' -o \
        -name '.env.*' -o \
        -name '*.pem' -o \
        -name '*.key' -o \
        -name '*.crt' -o \
        -name '*.cer' -o \
        -name '*.p12' -o \
        -name '*.pfx' -o \
        -name '*.jks' -o \
        -name '*.keystore' -o \
        -name credentials.json -o \
        -name 'service-account*.json' \
      \) \
    \) -print -quit | grep -q .; then
      echo "protected workspace or credential path reached Go build stage" >&2
      exit 1
    fi
  '
}

inspect_build_stage
if [[ "${GOHERMIT_ACCEPTANCE_INJECT_FAILURE_AFTER_BUILD_AUDIT:-}" == "1" ]]; then
  echo "injecting requested failure after build-stage audit" >&2
  exit 97
fi
if [[ "${GOHERMIT_ACCEPTANCE_INJECT_FAILURE_AFTER_BUILD_AUDIT:-}" == "TERM" ]]; then
  echo "injecting requested TERM after build-stage audit" >&2
  kill -TERM "$$"
  exit 143
fi
"${compose[@]}" build
"${compose[@]}" up -d

wait_for_workbench() {
  local attempts=0
  until curl --silent --show-error --fail "${base_url}/api/health" >"${acceptance_root}/health.json"; do
    attempts=$((attempts + 1))
    if (( attempts >= 60 )); then
      "${compose[@]}" logs
      return 1
    fi
    sleep 1
  done
  curl --silent --show-error --fail "${base_url}/api/info" >"${acceptance_root}/info.json"
  grep -q '"version":"0.8.0-dev"' "${acceptance_root}/info.json"
}

inspect_runtime_image() {
  local image_id
  image_id="$("${compose[@]}" images -q gohermit-web)"
  test -n "${image_id}"
  docker image inspect "${image_id}" >"${acceptance_root}/image.inspect.json"
  docker run --rm --entrypoint /bin/sh "${image_id}" -eu -c '
    for executable in node npm npx pnpm corepack go; do
      if command -v "${executable}" >/dev/null 2>&1; then
        echo "unexpected build executable in runtime image: ${executable}" >&2
        exit 1
      fi
    done
    test ! -e /src
    test ! -e /workspace/web
    if find / -xdev \( -type d \( -name node_modules -o -name .pnpm-store -o -name .npm \) -o -type f \( -name package.json -o -name pnpm-lock.yaml -o -name pnpm-workspace.yaml \) \) -print -quit | grep -q .; then
      echo "frontend source or package-manager state found in runtime image" >&2
      exit 1
    fi
  '
}

test_container_browser() {
  GOHERMIT_DOCKER_BASE_URL="${base_url}" pnpm test:e2e:docker
}

test_raw_path_security() {
  local path status
  for path in \
    '/%2e%2e/dashboard' \
    '/employees%2femployee-docker' \
    '/employees%5cemployee-docker'
  do
    status="$(curl --path-as-is --silent --show-error \
      --output "${acceptance_root}/raw-path.body" \
      --write-out '%{http_code}' "${base_url}${path}")"
    if [[ "${status}" != "404" ]]; then
      echo "raw path ${path} returned ${status}, want 404" >&2
      return 1
    fi
    if grep -q '<div id="root"></div>' "${acceptance_root}/raw-path.body"; then
      echo "raw path ${path} escaped into the React fallback" >&2
      return 1
    fi
  done
}

wait_for_workbench
inspect_runtime_image
test_container_browser
test_raw_path_security

manifest() {
  local root="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    find "${root}" -type f -print0 | sort -z | xargs -0 sha256sum
  else
    find "${root}" -type f -print0 | sort -z | xargs -0 shasum -a 256
  fi
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
inspect_build_stage
"${compose[@]}" up -d
wait_for_workbench
inspect_runtime_image
test_container_browser
test_raw_path_security
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
