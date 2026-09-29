#!/bin/sh
# Runs once, on first initialisation of the primary, before any migration.
# stator_app is subject to every RLS policy; stator_admin is exempt by policy,
# not by being superuser. The foundation migration only creates them when they
# are missing, and then without a password, so logging in needs this script.
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-SQL
    CREATE ROLE stator_app LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${APP_DB_PASSWORD:-stator_app}';
    CREATE ROLE stator_admin LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${ADMIN_DB_PASSWORD:-stator_admin}';

    GRANT CONNECT ON DATABASE "$POSTGRES_DB" TO stator_app, stator_admin;

    -- The connection pool watches pg_stat_wal_receiver to tell a caught-up
    -- standby from one whose receiver has silently dropped. Read-only stat
    -- access is the least privilege that answers that question.
    GRANT pg_read_all_stats TO stator_app, stator_admin;
SQL
