#!/usr/bin/env bash
# The Keycloak realm the stack imports is the suite's realm dynamo, the same
# file Armature and Exciter import (#411). Dynamo will hold the canonical
# copy; until it does, the canonical copy is Armature's realm at the commit
# api/armature/SOURCE pins, vendored beside its API document as
# api/armature/realm.json by make armature-openapi. The two must be the same
# bytes: there is no documented difference to tolerate.
#
# Needs bash, cmp and grep only; no stack and no network.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
OURS="deploy/keycloak/realm.json"
CANONICAL="api/armature/realm.json"
REALM="dynamo"

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }

echo "🧪 Keycloak realm"

if cmp -s "${SRC}/${OURS}" "${SRC}/${CANONICAL}"; then
  pass "${OURS} is the realm Armature imports at the pinned commit"
else
  fail "${OURS} differs from ${CANONICAL}, Armature's realm at the commit api/armature/SOURCE pins. Copy ${CANONICAL} over ${OURS}; a change Stator needs goes to Armature first (and to Dynamo once it holds the realm)."
fi

name="$(grep -m1 -oE '^  "realm": "[^"]*"' "${SRC}/${OURS}" | cut -d'"' -f4)"
[[ "$name" == "$REALM" ]] && pass "the realm is called ${REALM}" \
  || fail "${OURS} defines the realm '${name}', but the suite signs in through '${REALM}'."

# Everything that names a realm by URL names this one, so a rename cannot
# leave an issuer behind that Keycloak no longer answers.
stale=$(cd "$SRC" && grep -rnE '/realms/[A-Za-z0-9_-]+' deploy mk Makefile README.md docs/architecture.md \
  | grep -E 'localhost|keycloak' | grep -vE "/realms/(${REALM}|master)\b")
[[ -z "$stale" ]] && pass "every issuer of the stack names /realms/${REALM}" \
  || fail "these name another realm than ${REALM}: $(paste -sd' ' <<< "$stale")"

exit "$FAILED"
