#!/usr/bin/env bash
# The backup round trip against the running stack: back the live site up,
# restore the archive into a database and a bucket of their own, back those up
# again and compare the two archives entry by entry. Then the refusals: a
# database in use, a newer schema and another secret key, each checked by the
# database being left as it was. Run through make test-backup, which names the
# compose project.
set -uo pipefail

SRC="$(cd "$(dirname "$0")/.." && pwd)"
CURL_IMAGE="${CURL_IMAGE:-curlimages/curl:8.11.1}"
RESTORE_DB=stator_restore
RESTORE_BUCKET=stator-restore
PG_PASSWORD="${POSTGRES_PASSWORD:-stator}"
S3_KEY="${S3_ACCESS_KEY:-stator}"
S3_SECRET="${S3_SECRET_KEY:-stator-dev-secret}"
RESTORE_URL="postgres://stator:${PG_PASSWORD}@postgres-primary:5432/${RESTORE_DB}?sslmode=disable"
# 32 bytes in base64, and not the stack's key.
OTHER_KEY="b3RoZXIta2V5LW90aGVyLWtleS1vdGhlci1rZXktMTI="

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }

docker compose ps --status running --services 2>/dev/null | grep -qx api \
  || { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }

WORK="$(mktemp -d "${SRC}/.cache/backup-test.XXXXXX")"
mkdir -p "$WORK"/{site,restored,crafted}
psql() { docker compose exec -T postgres-primary psql -U stator -v ON_ERROR_STOP=1 -qAt "$@"; }

cleanup() {
  psql -d stator -c "DROP DATABASE IF EXISTS ${RESTORE_DB} WITH (FORCE)" >/dev/null 2>&1
  psql -d stator -c "DELETE FROM app_user WHERE email = '${MARK}@backup.test'" >/dev/null 2>&1
  s3 DELETE "stator-files/${MARK}/one.txt" >/dev/null 2>&1
  s3 DELETE "stator-files/${MARK}/two.bin" >/dev/null 2>&1
  rm -rf "$WORK"
}
trap cleanup EXIT

backup() { # dir args...
  local dir="$1"
  shift
  STATOR_BACKUP_DIR="$dir" docker compose run --rm --quiet-pull -u "$(id -u):$(id -g)" "$@"
}

s3() { # method bucket/key [file]
  docker run --rm --network "${COMPOSE_PROJECT_NAME}_default" ${3:+-v "$3:/body:ro"} "$CURL_IMAGE" \
    -fsS -X "$1" --aws-sigv4 "aws:amz:us-east-1:s3" --user "${S3_KEY}:${S3_SECRET}" \
    ${3:+-T /body} "http://seaweedfs:8333/$2"
}

# What an archive holds, independent of when it was taken: the dump without
# the per-run restrict key, and every object with its checksum. Parentheses go
# too: Postgres prints a restored check or trigger condition with fewer of
# them, though it is the same expression.
fingerprint() { # archive
  local archive="$1"
  tar -xOf "$archive" database.sql.gz | gunzip | grep -v '^\\\(un\)\?restrict ' | tr -d '()'
  tar -tf "$archive" | grep '^objects/' | sort | while read -r entry; do
    printf '%s %s\n' "$entry" "$(tar -xOf "$archive" "$entry" | sha256sum | cut -d' ' -f1)"
  done
}

echo "🧪 Backup round trip"
docker compose build -q backup || { echo "The backup image did not build."; exit 1; }

# The stack may hold no site at all, so the test brings a row and files of its
# own. An account rather than an organization: an instance holds one, and the
# stack may have it already.
MARK="backup-test-$$"
psql -d stator -c "INSERT INTO app_user (email, name) VALUES ('${MARK}@backup.test', 'Backup test')" >/dev/null \
  || { echo "Could not add the test's account."; exit 1; }
printf 'a file of the site\n' > "$WORK/one.txt"
head -c 70000 /dev/urandom > "$WORK/two.bin"
s3 PUT "stator-files/${MARK}/one.txt" "$WORK/one.txt" >/dev/null && s3 PUT "stator-files/${MARK}/two.bin" "$WORK/two.bin" >/dev/null \
  || { echo "Could not store the test's files in the stack's bucket."; exit 1; }

if backup "$WORK/site" backup create; then
  pass "backed up the running site"
else
  fail "backup create failed against the running site"
fi
SITE=$(ls "$WORK"/site/stator-*.tar 2>/dev/null | head -1)
[[ -n "$SITE" ]] || { echo "No archive was written."; exit 1; }
tar -tf "$SITE" | grep -qx "objects/${MARK}/two.bin" && pass "the archive holds the site's files" \
  || fail "the archive lacks objects/${MARK}/two.bin"

psql -d stator -c "DROP DATABASE IF EXISTS ${RESTORE_DB} WITH (FORCE)" -c "CREATE DATABASE ${RESTORE_DB}" \
  -c "GRANT CONNECT ON DATABASE ${RESTORE_DB} TO stator_app, stator_admin" >/dev/null
RESTORE_ENV=(-e "STATOR_DB_PRIMARY_URL=${RESTORE_URL}" -e "STATOR_S3_BUCKET=${RESTORE_BUCKET}")

if backup "$WORK/site" "${RESTORE_ENV[@]}" backup restore -yes; then
  pass "restored the archive into a database and a bucket of their own"
else
  fail "backup restore failed into an empty database"
fi
backup "$WORK/restored" "${RESTORE_ENV[@]}" backup create >/dev/null || fail "backup create failed against the restored site"
RESTORED=$(ls "$WORK"/restored/stator-*.tar 2>/dev/null | head -1)
if [[ -n "$RESTORED" ]] && diff <(fingerprint "$SITE") <(fingerprint "$RESTORED") > "$WORK/diff"; then
  pass "the restored site backs up to the same database and files"
else
  fail "the restored site differs from the backup: $(head -c 2000 "$WORK/diff")"
fi
migrations() { tar -xOf "$1" manifest.json | sed -n '/"migrations"/,/]/p'; }
[[ "$(migrations "$SITE")" == "$(migrations "$RESTORED")" ]] \
  && pass "the restored site has the same schema version" || fail "the schema versions differ"

accounts=$(psql -d "$RESTORE_DB" -c "SELECT count(*) FROM app_user WHERE email = '${MARK}@backup.test'")
app_accounts=$(docker compose exec -T -e PGPASSWORD="${APP_DB_PASSWORD:-stator_app}" postgres-primary \
  psql -h 127.0.0.1 -U stator_app -d "$RESTORE_DB" -qAt -c "SELECT count(*) FROM app_user" 2>&1)
if [[ "$accounts" == 1 && "$app_accounts" == 0 ]]; then
  pass "row level security holds on the restored rows: the app role sees no account without a tenant"
else
  fail "restored $accounts of the test's account, and the app role saw [$app_accounts] accounts without a tenant"
fi

# A refusal must change nothing; the marker shows the database was not replaced.
psql -d "$RESTORE_DB" -c "CREATE TABLE public.restore_marker ()" >/dev/null
refused() { # description expected args...
  local what="$1" expected="$2" out
  shift 2
  if out=$(backup "$WORK/crafted" "$@" 2>&1); then
    fail "$what: the restore went ahead"
  elif ! grep -qF "$expected" <<< "$out"; then
    fail "$what: refused without saying [$expected]: $out"
  elif [[ "$(psql -d "$RESTORE_DB" -c "SELECT to_regclass('public.restore_marker') IS NOT NULL")" != t ]]; then
    fail "$what: refused, but the database was replaced"
  else
    pass "$what is refused and changes nothing"
  fi
}

cp "$SITE" "$WORK/crafted/stator-20000101T000000Z.tar"
refused "restoring without -yes" "-yes" "${RESTORE_ENV[@]}" backup restore
refused "restoring under another secret key" "not the key" "${RESTORE_ENV[@]}" -e "STATOR_SECRET_KEY=${OTHER_KEY}" backup restore -yes

(cd "$WORK/crafted" && tar -xf stator-20000101T000000Z.tar && rm stator-20000101T000000Z.tar \
  && sed -i 's/"migrations": \[/"migrations": [\n    99999,/' manifest.json \
  && tar -cf stator-20000101T000000Z.tar manifest.json database.sql.gz $(ls -d objects 2>/dev/null) \
  && rm -rf manifest.json database.sql.gz objects)
refused "restoring a backup from a newer schema" "migration 99999" "${RESTORE_ENV[@]}" backup restore -yes

docker compose exec -T -d -e PGPASSWORD="${APP_DB_PASSWORD:-stator_app}" postgres-primary \
  psql -h 127.0.0.1 -U stator_app -d "$RESTORE_DB" -qAt -c "SELECT pg_sleep(60)"
for _ in $(seq 1 20); do
  [[ "$(psql -d stator -c "SELECT count(*) FROM pg_stat_activity WHERE datname = '${RESTORE_DB}' AND usename = 'stator_app'")" != 0 ]] && break
  sleep 0.5
done
cp "$SITE" "$WORK/crafted/stator-20000101T000000Z.tar"
refused "restoring while the app is connected" "in use by stator_app" "${RESTORE_ENV[@]}" backup restore -yes

(( FAILED == 0 )) && echo "Backup round trip passed." || echo "Backup round trip failed."
exit "$FAILED"
