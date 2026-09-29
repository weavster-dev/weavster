#!/usr/bin/env bash
# Runs docs/getting-started.md against the Docker Compose stack, as CI's
# compose job does after `docker compose up`. The lines between the markers
# are the guide's commands, word for word (TestGettingStartedDoc checks);
# name=$(command) keeps a command's reply to check it below.
# Usage: scripts/getting-started.sh   (from the repository root, stack running)
set -euo pipefail

# --- the guide ---
until curl -fsS http://127.0.0.1:8080/api/openapi.yaml >/dev/null; do sleep 1; done
go build -o bin/weavster ./cmd/weavster
bin/weavster test examples/getting-started
bin/weavster -a http://127.0.0.1:8080 -u admin -p Weavster-dev-1 -s examples/getting-started/setup.txt
reply=$(curl -s -u admin:Weavster-dev-1 -H 'X-Weavster-CSRF: 1' http://127.0.0.1:8080/api/v1/flows/hello/messages -d '{"patient":{"name":"Ada Lovelace","mrn":"12345"}}')
# --- end ---

# The guide's message was processed: sent, and its transformed content has
# the greeting.
echo "$reply"
case $reply in
*'"status":"sent"'*) ;;
*) echo "the message was not sent: $reply" >&2; exit 1 ;;
esac
id=$(printf '%s' "$reply" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
content=$(curl -fsS -u admin:Weavster-dev-1 -H 'X-Weavster-CSRF: 1' "http://127.0.0.1:8080/api/v1/messages/$id/content?part=transformed")
echo "$content"
case $content in
*'"greeting":{"mrn":"12345","to":"Ada Lovelace"}'*) ;;
*) echo "unexpected transformed content: $content" >&2; exit 1 ;;
esac
echo "getting started passed"
