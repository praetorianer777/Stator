#!/usr/bin/env bash
# Kroki in the running stack: the api's container draws a PlantUML diagram
# through it, secure mode keeps a diagram from reading a file, and Kroki itself
# reaches nothing outside its network. Drawing through the api's own routes
# is the browser suite's (e2e/tests/diagrams.spec.ts). Run through make
# test-kroki, which names the compose project.
set -uo pipefail

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }

running=$(docker compose ps --status running --services 2>/dev/null)
for service in api kroki; do
  grep -qx "$service" <<<"$running" \
    || { echo "The stack for this checkout has no running $service. Start it with make up or make stack-up, then run this again."; exit 1; }
done

draw() { # language source
  docker compose exec -T api wget -q -O - -T 10 --header 'Content-Type: text/plain' \
    --post-data "$2" "http://kroki:8000/$1/svg" 2>&1
}

echo "🧪 Kroki draws for the api and reaches nothing else"

svg=$(draw plantuml $'@startuml\nAlice -> Bob: hello\n@enduml')
if grep -q '<svg' <<<"$svg" && grep -q 'Alice' <<<"$svg"; then
  pass "the api's container gets an SVG for a PlantUML diagram"
else
  fail "the api's container got no SVG for a PlantUML diagram: ${svg:0:300}"
fi

# In secure mode the include is dropped without a word and the rest draws.
svg=$(draw plantuml $'@startuml\n!include /etc/passwd\nAlice -> Bob: hello\n@enduml')
if grep -q '<svg' <<<"$svg" && ! grep -q 'root:' <<<"$svg"; then
  pass "secure mode keeps PlantUML from reading Kroki's files"
else
  fail "a PlantUML include of /etc/passwd was not dropped: ${svg:0:300}"
fi

# Probed from inside Kroki's network namespace with the backend image's wget,
# since Kroki's own image is not ours to rely on for a client.
kroki=$(docker compose ps -q kroki)
probe() { # url
  docker run --rm --network "container:${kroki}" --entrypoint wget "${COMPOSE_PROJECT_NAME}-backend" \
    -q -O /dev/null -T 5 "$1" 2>&1
}

if probe http://localhost:8000/health >/dev/null; then
  pass "the probe reaches Kroki from its own network"
else
  fail "the probe could not reach Kroki on localhost, so the checks below would prove nothing"
fi
if out=$(probe http://1.1.1.1/); then
  fail "Kroki reached an address outside the host"
else
  pass "Kroki reaches no address outside the host (${out:-refused})"
fi
if out=$(probe http://web/); then
  fail "Kroki reached the web container, which is not on its network"
else
  pass "Kroki reaches no other service of the stack (${out:-refused})"
fi

(( FAILED == 0 )) && echo "Kroki checks passed." || echo "Kroki checks failed."
exit "$FAILED"
