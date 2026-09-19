#!/usr/bin/env bash
# Charge les donnees de test PII (FR/US) dans les bases SOURCE de la stack de dev.
#
# Usage :
#   ./scripts/testdata/seed-pii-testdata.sh            # toutes les bases disponibles
#   ./scripts/testdata/seed-pii-testdata.sh postgres   # une seule cible
#
# Cibles : postgres | mysql | mssql | mongo
# Les conteneurs doivent tourner (compose.dev.yml + compose/compose-db-*.yml).
# Une cible dont le conteneur est absent est ignoree avec un avertissement.

set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGETS=("$@")
if [ ${#TARGETS[@]} -eq 0 ]; then
  TARGETS=(postgres mysql mssql mongo)
fi

ok=0
skipped=0
failed=0

running() {
  [ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null)" = "true" ]
}

seed_postgres() {
  local c=test-prod-db
  running "$c" || return 2
  docker exec -i "$c" psql -v ON_ERROR_STOP=1 -q -U postgres -d postgres \
    < "$DIR/pii-testdata.postgres.sql"
}

seed_mysql() {
  local c=test-prod-db-mysql
  running "$c" || return 2
  # MySQL peut refuser les connexions pendant son initialisation : on patiente.
  local i=0
  until docker exec "$c" mysqladmin ping -h127.0.0.1 -uroot -prootpassword --silent 2>/dev/null; do
    i=$((i + 1))
    [ "$i" -gt 30 ] && { echo "    mysql injoignable apres 60s"; return 1; }
    sleep 2
  done
  docker exec -i "$c" mysql -uroot -prootpassword mydatabase \
    < "$DIR/pii-testdata.mysql.sql" 2>&1 | grep -v '^mysql: \[Warning\]'
  return "${PIPESTATUS[0]}"
}

seed_mssql() {
  local c=test-prod-db-mssql
  running "$c" || return 2
  local pw='YourStrong@Passw0rd'
  # Le binaire sqlcmd a change de chemin selon les versions d'image.
  local bin
  for candidate in /opt/mssql-tools18/bin/sqlcmd /opt/mssql-tools/bin/sqlcmd; do
    if docker exec "$c" test -x "$candidate" 2>/dev/null; then bin="$candidate"; break; fi
  done
  if [ -z "${bin:-}" ]; then
    echo "    sqlcmd absent de l'image -- cible ignoree"
    return 2
  fi
  # SQL Server met ~30s a accepter les connexions apres demarrage.
  local i=0
  until docker exec "$c" "$bin" -C -S localhost -U sa -P "$pw" -Q "SELECT 1" >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -gt 40 ] && { echo "    mssql injoignable apres 80s"; return 1; }
    sleep 2
  done
  docker exec "$c" "$bin" -C -S localhost -U sa -P "$pw" \
    -Q "IF DB_ID('testdb') IS NULL CREATE DATABASE testdb" >/dev/null || return 1
  docker exec -i "$c" "$bin" -C -S localhost -U sa -P "$pw" -d testdb -b \
    < "$DIR/pii-testdata.mssql.sql" >/dev/null
}

seed_mongo() {
  local c=test-prod-db-mongo
  running "$c" || return 2
  local sh=mongosh
  docker exec "$c" which mongosh >/dev/null 2>&1 || sh=mongo
  # insertMany renvoie la liste des ObjectId : bruit inutile, on ne garde que le total.
  docker exec -i "$c" "$sh" --quiet < "$DIR/pii-testdata.mongo.js" \
    | grep -E '^clients: ' || true
  return "${PIPESTATUS[0]}"
}

for t in "${TARGETS[@]}"; do
  echo "==> $t"
  case "$t" in
    postgres) seed_postgres ;;
    mysql)    seed_mysql ;;
    mssql)    seed_mssql ;;
    mongo)    seed_mongo ;;
    *)        echo "    cible inconnue : $t"; failed=$((failed + 1)); continue ;;
  esac
  rc=$?
  case $rc in
    0) echo "    OK -- clients + donnees_brutes (24 lignes chacune)"; ok=$((ok + 1)) ;;
    2) echo "    conteneur absent ou arrete -- ignore"; skipped=$((skipped + 1)) ;;
    *) echo "    ECHEC (code $rc)"; failed=$((failed + 1)) ;;
  esac
done

echo
echo "Termine : $ok charge(s), $skipped ignore(s), $failed echec(s)."
[ "$failed" -gt 0 ] && exit 1
exit 0
