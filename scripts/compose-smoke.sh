#!/usr/bin/env bash
# Smoke test of docker-compose.yml: start the stack, create a flow over the
# API, then restart the server and recreate the containers, checking each
# time that the flow was kept in PostgreSQL (the named volume).
# Usage: scripts/compose-smoke.sh   (leaves the stack running; `docker compose down -v` removes it)
set -euo pipefail

base=http://127.0.0.1:8080
api() { curl -fsS -u admin:Weavster-dev-1 -H 'X-Weavster-CSRF: 1' "$@"; }
fail() {
  echo "$1" >&2
  docker compose logs >&2
  exit 1
}
wait_up() {
  for _ in $(seq 1 60); do
    if curl -fsS "$base/api/openapi.yaml" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  fail "server did not come up"
}

docker compose up -d --build --wait
wait_up
code=$(curl -sS -o /dev/null -w '%{http_code}' -u admin:Weavster-dev-1 -H 'X-Weavster-CSRF: 1' \
  -X POST "$base/api/v1/flows" -H 'Content-Type: application/json' \
  -d '{"id":"compose-smoke","name":"Compose smoke"}')
# 409: the flow is there from an earlier run.
[ "$code" = 201 ] || [ "$code" = 409 ] || fail "creating the flow answered $code"

check() {
  wait_up
  body=$(api "$base/api/v1/flows/compose-smoke") || fail "the flow was lost $1"
  case $body in
  *'"name":"Compose smoke"'*) ;;
  *) fail "the flow was lost $1: $body" ;;
  esac
}
docker compose restart weavster
check "on a server restart"
docker compose down
docker compose up -d --wait
check "on down and up (the database volume)"
echo "compose smoke test passed"
