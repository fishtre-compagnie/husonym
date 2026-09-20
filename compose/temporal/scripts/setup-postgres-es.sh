#!/bin/sh
# Creates or upgrades the Temporal schemas in PostgreSQL and Elasticsearch.
# Taken from temporalio/samples-server (compose/scripts/setup-postgres-es.sh),
# without the curl fallback that admin-tools 1.30+ no longer needs. Every
# command is idempotent, so it runs on each start as auto-setup did.
set -eu

: "${ES_SCHEME:?ERROR: ES_SCHEME environment variable is required}"
: "${ES_HOST:?ERROR: ES_HOST environment variable is required}"
: "${ES_PORT:?ERROR: ES_PORT environment variable is required}"
: "${ES_VISIBILITY_INDEX:?ERROR: ES_VISIBILITY_INDEX environment variable is required}"
: "${POSTGRES_SEEDS:?ERROR: POSTGRES_SEEDS environment variable is required}"
: "${POSTGRES_USER:?ERROR: POSTGRES_USER environment variable is required}"

echo 'Waiting for PostgreSQL port to be available...'
nc -z -w 10 "${POSTGRES_SEEDS}" "${DB_PORT:-5432}"

sql_tool() {
  temporal-sql-tool --plugin postgres12 --ep "${POSTGRES_SEEDS}" -u "${POSTGRES_USER}" -p "${DB_PORT:-5432}" --db temporal "$@"
}
sql_tool create
sql_tool setup-schema -v 0.0
sql_tool update-schema -d /etc/temporal/schema/postgresql/v12/temporal/versioned

temporal-elasticsearch-tool --ep "${ES_SCHEME}://${ES_HOST}:${ES_PORT}" setup-schema
temporal-elasticsearch-tool --ep "${ES_SCHEME}://${ES_HOST}:${ES_PORT}" create-index --index "${ES_VISIBILITY_INDEX}"

echo 'PostgreSQL and Elasticsearch setup complete'
