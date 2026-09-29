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

echo "⎈ CloudNativePG with one instance"
render "cnpg, 1 instance" --set cnpg.enabled=true --set cnpg.spec.instances=1
check "writes go to -rw" "$(env_value "${RENDERED}" STATOR_DB_PRIMARY_URL)" "$(app_url "${RW}" require)"
check "no replica URL, so reads go to -rw" "$(env_value "${RENDERED}" STATOR_DB_REPLICA_URLS)" ""
check "-ro is named nowhere" "$(grep -c -- "${RO}" <<<"${RENDERED}")" "0"

echo "⎈ CloudNativePG with read replicas turned off"
render "cnpg, 3 instances, readReplicas off" --set cnpg.enabled=true --set cnpg.readReplicas=false
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
    --set database.host=
refused "postgres as the owner under cnpg" "database.ownerRole is postgres, which CNPG keeps for its own superuser" \
    --set cnpg.enabled=true --set database.ownerRole=postgres "${VALKEY[@]}"
refused "the seed in production" "jobs.seed.enabled is true but env is production" \
    --set database.host=db.example --set jobs.seed.enabled=true

if [[ ${FAILED} -ne 0 ]]; then
    echo "❌ chart tests failed"
    exit 1
fi
echo "✅ chart tests passed"
