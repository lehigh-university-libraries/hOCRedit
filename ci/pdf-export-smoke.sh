#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${PDF_EXPORT_TEST_IMAGE:-scribe-pdf-export:test}"
work_dir="$(mktemp -d)"
container_id=""
test_image="scribe-pdf-runtime-test:$$"
cleanup() {
  if [ -n "$container_id" ]; then docker rm -f "$container_id" >/dev/null 2>&1 || true; fi
  docker image rm "$test_image" >/dev/null 2>&1 || true
  rm -rf "$work_dir"
}
trap cleanup EXIT

cd "$repo_root"
docker compose -f docker-compose.yaml config --format json | jq -e '
  .networks["pdf-export"].internal == true and
  (.services["pdf-export"].networks | keys) == ["pdf-export"] and
  (.services["pdf-export"].ports // [] | length) == 0 and
  (.services["pdf-export"].secrets // [] | length) == 0 and
  (.services["pdf-export"].volumes // [] | length) == 0 and
  .services["pdf-export"].image == .services.api.image and
  (.services.api.networks | has("pdf-export"))
' >/dev/null
docker build -t "$image" .
architecture="$(docker image inspect "$image" --format '{{.Architecture}}')"
CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" go test -c -o "$work_dir/pdf-runtime-test" ./internal/pdfexport
cat > "$work_dir/Dockerfile" <<'DOCKERFILE'
ARG PDF_RUNTIME_IMAGE=scribe-pdf-export:test
FROM ${PDF_RUNTIME_IMAGE}
COPY pdf-runtime-test /app/pdf-runtime-test
DOCKERFILE
docker build --build-arg "PDF_RUNTIME_IMAGE=$image" -t "$test_image" "$work_dir"
container_id="$(docker create --read-only --network none --cap-drop ALL \
  --security-opt no-new-privileges --memory 1g --pids-limit 64 \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=1g,mode=1777 \
  --env SCRIBE_PDF_RUNTIME_TEST=true --env PYTHONDONTWRITEBYTECODE=1 \
  --entrypoint /app/pdf-runtime-test "$test_image" -test.v)"
docker start -a "$container_id"
