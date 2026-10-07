#!/usr/bin/env bash
# Tests for deploy/charts/stator: helm lint and helm template for CloudNativePG
# with one and with three instances, for a database of one's own, for the
# bundled one, and for the settings the chart must refuse. Helm runs in a
# container; the rendered database URLs are checked host by host.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
CHART="${SRC}/deploy/charts/stator"
HELM_IMAGE="${HELM_IMAGE:-alpine/helm:3.19.0}"
RELEASE=r
FULL="${RELEASE}-stator"

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

helm() {
    docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "${CHART}:/chart:ro" "${HELM_IMAGE}" "$@"
}

# The one value the api, worker and seed are given for an environment variable;
# more than one means two workloads disagree about where the database is. The
# migrate Job's owner URL is checked on its own by migrate_url.
env_value() { # rendered name
    grep -A1 -- "- name: $2\$" <<<"$1" | sed -n 's/^ *value: "\(.*\)"$/\1/p' | grep -vF 'DB_OWNER_PASSWORD' | sort -u | paste -sd ' '
}

migrate_url() { # rendered
    grep -A1 -- '- name: STATOR_DB_PRIMARY_URL$' <<<"$1" | grep -F DB_OWNER_PASSWORD | sed -n 's/^ *value: "\(.*\)"$/\1/p' | sort -u
}

# lint and template with the same settings, which must both succeed.
render() { # description args...
    local what="$1"
    shift
    if out=$(helm lint /chart --strict "$@" 2>&1); then
        pass "${what}: helm lint"
    else
        fail "${what}: helm lint"
        echo "${out}" | sed 's/^/      /'
    fi
    if RENDERED=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        pass "${what}: helm template"
        # The api serves them only for the browser suite; a deployment must
        # never switch them on, nor even hand it a token.
        check "${what}: the test endpoints stay off" "$(grep -c 'STATOR_TEST_ENDPOINTS' <<<"${RENDERED}")" "0"
        # Only the compose stack trades commit durability for speed.
        check "${what}: commits wait for the disk" "$(grep -c 'synchronous_commit' <<<"${RENDERED}")" "0"
        # The stub stands in for Armature in the test stacks alone.
        check "${what}: no armature-stub" "$(grep -c 'armature-stub' <<<"${RENDERED}")" "0"
    else
        fail "${what}: helm template"
        echo "${RENDERED}" | sed 's/^/      /'
        RENDERED=""
    fi
}

refused() { # description expected-message args...
    local what="$1" message="$2"
    shift 2
    if out=$(helm template "${RELEASE}" /chart "$@" 2>&1); then
        fail "${what}: rendered, but should have been refused"
    elif grep -qF -- "${message}" <<<"${out}"; then
        pass "${what}"
    else
        fail "${what}: refused without saying [${message}]"
        echo "${out}" | sed 's/^/      /'
    fi
}

app_url() { echo "postgres://stator_app:\$(DB_APP_PASSWORD)@$1:5432/stator?sslmode=$2"; }
admin_url() { echo "postgres://stator_admin:\$(DB_ADMIN_PASSWORD)@$1:5432/stator?sslmode=$2"; }
owner_url() { echo "postgres://$1:\$(DB_OWNER_PASSWORD)@$2:5432/stator?sslmode=$3"; }

RW="${FULL}-postgres-rw"
RO="${FULL}-postgres-ro"
VALKEY=(--set valkey.host=valkey.example)

echo "⎈ CloudNativePG with three instances"
render "cnpg, 3 instances" --set cnpg.enabled=true "${VALKEY[@]}"
check "writes go to -rw" "$(env_value "${RENDERED}" STATOR_DB_PRIMARY_URL)" "$(app_url "${RW}" require)"
check "admin work goes to -rw" "$(env_value "${RENDERED}" STATOR_DB_ADMIN_URL)" "$(admin_url "${RW}" require)"
check "reads go to -ro" "$(env_value "${RENDERED}" STATOR_DB_REPLICA_URLS)" "$(app_url "${RO}" require)"
check "migrations run as the owner on -rw" \
    "$(migrate_url "${RENDERED}")" \
    "$(owner_url stator_owner "${RW}" require)"
check "the Cluster is rendered with three instances" "$(grep -c '^kind: Cluster$' <<<"${RENDERED}") $(grep -c '^  instances: 3$' <<<"${RENDERED}")" "1 1"
check "the Cluster keeps commit timestamps" "$(grep -c 'track_commit_timestamp: "on"' <<<"${RENDERED}")" "1"
check "the operator makes both runtime roles" "$(grep -cE '^    - (name: stator_app|name: stator_admin)|^      name: stator_(app|admin)$' <<<"${RENDERED}")" "2"
check "the pools are sized apart" \
    "$(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set database.pool.primaryMaxConns=15 --set database.pool.replicaMaxConns=45 2>&1 | grep -E 'STATOR_DB_(PRIMARY|REPLICA)_MAX_CONNS' | tr -d ' ' | paste -sd ' ')" \
    'STATOR_DB_PRIMARY_MAX_CONNS:"15" STATOR_DB_REPLICA_MAX_CONNS:"45"'
check "the Valkey URL reaches every workload" "$(env_value "${RENDERED}" STATOR_VALKEY_URL)" "redis://valkey.example:6379/0"
check "the upload limit is set, and can be changed" \
    "$(grep -c 'STATOR_UPLOAD_LIMIT: "50MB"' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set attachments.uploadLimit=2GB 2>&1 | grep -c 'STATOR_UPLOAD_LIMIT: "2GB"')" \
    "1 1"
check "office previews are off until a converter is named" \
    "$(grep -c 'STATOR_CONVERTER_URL' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set attachments.converterUrl=http://converter:3000 2>&1 | grep -c 'STATOR_CONVERTER_URL: "http://converter:3000"')" \
    "0 1"
check "the audit log is kept a year, and that can be changed" \
    "$(grep -c 'STATOR_RETAIN_AUDIT: "8760h"' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set retention.audit=2160h 2>&1 | grep -c 'STATOR_RETAIN_AUDIT: "2160h"')" \
    "1 1"
check "page views are named for a season, and that can be changed" \
    "$(grep -c 'STATOR_RETAIN_PAGE_VIEWS: "2160h"' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set retention.pageViews=720h 2>&1 | grep -c 'STATOR_RETAIN_PAGE_VIEWS: "720h"')" \
    "1 1"
check "verifications are checked every ten minutes, and that can be changed" \
    "$(grep -c 'STATOR_VERIFICATION_CHECK_INTERVAL: "10m"' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set verification.checkInterval=1h 2>&1 | grep -c 'STATOR_VERIFICATION_CHECK_INTERVAL: "1h"')" \
    "1 1"
check "due tasks are looked for every ten minutes, and that can be changed" \
    "$(grep -c 'STATOR_TASK_DUE_CHECK_INTERVAL: "10m"' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set tasks.dueCheckInterval=1h 2>&1 | grep -c 'STATOR_TASK_DUE_CHECK_INTERVAL: "1h"')" \
    "1 1"
check "scheduled publishes are looked for every half minute, and that can be changed" \
    "$(grep -c 'STATOR_SCHEDULE_CHECK_INTERVAL: "30s"' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set publishing.scheduleCheckInterval=5s 2>&1 | grep -c 'STATOR_SCHEDULE_CHECK_INTERVAL: "5s"')" \
    "1 1"
check "mail is off until a relay is named, then goes from the sender set" \
    "$(grep -c 'STATOR_SMTP_ADDR' <<<"${RENDERED}") $(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set mail.smtpAddr=smtp.example:25 --set 'mail.from=Wiki <wiki@example.com>' 2>&1 | grep -E 'STATOR_(SMTP_ADDR|MAIL_FROM)' | tr -d ' ' | paste -sd ' ')" \
    '0 STATOR_SMTP_ADDR:"smtp.example:25" STATOR_MAIL_FROM:"Wiki<wiki@example.com>"'

check "nothing inside the network is reached until named" "$(grep -cE 'STATOR_(OUTBOUND_ALLOW|ARMATURE_BACKCHANNEL)' <<<"${RENDERED}")" "0"
check "the guard lets through what is named, and Armature is reached where it is mapped" \
    "$(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set 'network.outboundAllow={armature-api.armature.svc,10.40.0.0/16}' \
        --set 'armature.backchannel.https://armature\.example\.com=http://armature-api.armature.svc:8080' 2>&1 \
        | grep -E 'STATOR_(OUTBOUND_ALLOW|ARMATURE_BACKCHANNEL)' | tr -d ' ' | paste -sd ' ')" \
    'STATOR_OUTBOUND_ALLOW:"armature-api.armature.svc,10.40.0.0/16" STATOR_ARMATURE_BACKCHANNEL:"https://armature.example.com=http://armature-api.armature.svc:8080"'

check "PDF export is off until the renderer is enabled" \
    "$(grep -c 'STATOR_RENDER_URL' <<<"${RENDERED}") $(grep -c "name: ${FULL}-render\$" <<<"${RENDERED}")" \
    "0 0"
RENDER_ON=$(helm template "${RELEASE}" /chart --set cnpg.enabled=true "${VALKEY[@]}" --set render.enabled=true --set render.timeout=15s 2>&1)
check "the api prints through the renderer's Service, within its bounds" \
    "$(grep -E 'STATOR_RENDER_(URL|TIMEOUT|CONCURRENCY|MAX_SIZE)' <<<"${RENDER_ON}" | tr -d ' ' | paste -sd ' ')" \
    "STATOR_RENDER_URL:\"http://${FULL}-render:8090\" STATOR_RENDER_TIMEOUT:\"15s\" STATOR_RENDER_CONCURRENCY:\"4\" STATOR_RENDER_MAX_SIZE:\"50MB\""
check "the renderer runs as a Deployment behind a Service of its own" \
    "$(grep -c "name: ${FULL}-render\$" <<<"${RENDER_ON}")" "2"
check "the renderer prints the web pods' pages inside the cluster" \
    "$(grep -A1 -- '- name: RENDER_APP_URL$' <<<"${RENDER_ON}" | sed -n 's/^ *value: "\(.*\)"$/\1/p')" "http://${FULL}-web"
check "the renderer keeps nothing but its scratch space" \
    "$(awk -v RS='---' -v name="name: ${FULL}-render" '/kind: Deployment/ && index($0, name "\n")' <<<"${RENDER_ON}" | grep -cE 'readOnlyRootFilesystem: true|mountPath: /tmp')" "2"
check "the web's nginx logs and caches no print view" \
    "$(grep -A3 -F 'location ^~ /print/ {' <<<"${RENDERED}" | grep -cE 'access_log off|Cache-Control "no-store"')" "2"

check "the web's nginx passes a shared draft's WebSocket on" \
    "$(grep -A4 -F 'location ~ ^/api/v1/pages/[^/]+/collab$' <<<"${RENDERED}" | grep -cE 'proxy_http_version 1.1|proxy_set_header Upgrade \$http_upgrade|proxy_set_header Connection "upgrade"')" \
    "3"

echo "⎈ One api pod without Valkey"
render "one api pod, no Valkey" --set database.host=db.example --set api.replicas=1
check "no Valkey URL" "$(env_value "${RENDERED}" STATOR_VALKEY_URL)" ""

# Editors on different pods pass each other's changes through Postgres then.
echo "⎈ Several api pods without Valkey or replicas"
render "three api pods, no Valkey" --set database.host=db.example --set api.replicas=3
check "no Valkey URL" "$(env_value "${RENDERED}" STATOR_VALKEY_URL)" ""

echo "⎈ CloudNativePG with one instance"
render "cnpg, 1 instance" --set cnpg.enabled=true --set cnpg.spec.instances=1 "${VALKEY[@]}"
check "writes go to -rw" "$(env_value "${RENDERED}" STATOR_DB_PRIMARY_URL)" "$(app_url "${RW}" require)"
check "no replica URL, so reads go to -rw" "$(env_value "${RENDERED}" STATOR_DB_REPLICA_URLS)" ""
check "-ro is named nowhere" "$(grep -c -- "${RO}" <<<"${RENDERED}")" "0"

echo "⎈ CloudNativePG with read replicas turned off"
render "cnpg, 3 instances, readReplicas off" --set cnpg.enabled=true --set cnpg.readReplicas=false "${VALKEY[@]}"
check "no replica URL" "$(env_value "${RENDERED}" STATOR_DB_REPLICA_URLS)" ""

echo "⎈ A database of one's own"
render "cnpg off" --set database.host=db.example --set "database.replicaHosts={db-ro.example}" "${VALKEY[@]}"
check "writes go to database.host" "$(env_value "${RENDERED}" STATOR_DB_PRIMARY_URL)" "$(app_url db.example require)"
check "reads go to database.replicaHosts" "$(env_value "${RENDERED}" STATOR_DB_REPLICA_URLS)" "$(app_url db-ro.example require)"
check "migrations run as postgres there" \
    "$(migrate_url "${RENDERED}")" \
    "$(owner_url postgres db.example require)"
check "no Cluster is rendered" "$(grep -c '^kind: Cluster$' <<<"${RENDERED}")" "0"

echo "⎈ The demo values, with the bundled Postgres and Valkey"
render "demo" -f /chart/values-demo.yaml
check "writes go to the bundled Postgres" "$(env_value "${RENDERED}" STATOR_DB_PRIMARY_URL)" "$(app_url "${FULL}-postgresql" disable)"
check "the bundled Valkey is used with its password" "$(env_value "${RENDERED}" STATOR_VALKEY_URL)" "redis://:\$(VALKEY_PASSWORD)@${FULL}-valkey:6379/0"

echo "⎈ What the chart refuses"
refused "cnpg and the bundled Postgres together" \
    "cnpg.enabled and postgresql.enabled are both true. Turn one of them off" \
    --set cnpg.enabled=true --set postgresql.enabled=true "${VALKEY[@]}"
refused "replicas behind several api pods without Valkey" \
    "read-your-writes needs a Valkey all api pods share" \
    --set cnpg.enabled=true
refused "no database at all" "Set database.host, or enable cnpg or the bundled postgresql" \
    --set database.host= "${VALKEY[@]}"
refused "postgres as the owner under cnpg" "database.ownerRole is postgres, which CNPG keeps for its own superuser" \
    --set cnpg.enabled=true --set database.ownerRole=postgres "${VALKEY[@]}"
refused "the seed in production" "jobs.seed.enabled is true but env is production" \
    --set database.host=db.example --set jobs.seed.enabled=true "${VALKEY[@]}"

if [[ ${FAILED} -ne 0 ]]; then
    echo "❌ chart tests failed"
    exit 1
fi
echo "✅ chart tests passed"
