#!/usr/bin/env bash
# Uzak PostgreSQL'e (ör. Neon) şema + demo verisi yükler. Yerelde Docker ister; imajlar compose ile derlenir.
#
#   OWNER_URL='postgres://neondb_owner:…@ep-xxx.eu-central-1.aws.neon.tech/neondb?sslmode=require' \
#   APP_DB_PASSWORD='güçlü-bir-parola' \
#   ./deploy/remote/migrate-seed.sh
#
# Çıktının sonunda API için DATABASE_URL yazdırılır (kısıtlı rol sinavplan_app). Sırlar dosyaya yazılmaz.
# Tekrar çalıştırmak güvenlidir: migrasyonlar idempotent, seed aynı veri varsa atlar.
set -euo pipefail
cd "$(dirname "$0")/../.."
: "${OWNER_URL:?OWNER_URL gerekli (sahip rol bağlantısı)}"
: "${APP_DB_PASSWORD:?APP_DB_PASSWORD gerekli (uygulama rolü parolası, en az 12 karakter)}"
[ "${#APP_DB_PASSWORD}" -ge 12 ] || { echo "APP_DB_PASSWORD en az 12 karakter olmalı" >&2; exit 1; }
APP_DB_USER="${APP_DB_USER:-sinavplan_app}"
SEED="${SEED:-42}"
DEMO_PASSWORD="${DEMO_PASSWORD:-Demo!1234}"
NET=()
[ -n "${DOCKER_NETWORK:-}" ] && NET=(--network "$DOCKER_NETWORK")   # yerel deneme için (compose ağı)

[ -f .env ] || cp .env.example .env
docker compose build migrate seed >/dev/null
echo "== migrate up"
docker run --rm "${NET[@]}" -e MIGRATE_DATABASE_URL="$OWNER_URL" -e APP_DB_USER="$APP_DB_USER" -e APP_DB_PASSWORD="$APP_DB_PASSWORD" \
  -e APP_ENV=production -e LOG_LEVEL=info sinavplan-migrate
echo "== seed (SEED=$SEED)"
docker run --rm "${NET[@]}" -e DATABASE_URL="$OWNER_URL" -e DEMO_PASSWORD="$DEMO_PASSWORD" -e LOG_LEVEL=info \
  -e SIM_EXAMS=300 -e SIM_STUDENTS=8000 -e SIM_ROOMS=60 sinavplan-seed seed --seed="$SEED"

# Sahip URL'sindeki kullanıcı:parola kısmını uygulama rolüyle değiştir (python yoksa elle yazın).
APP_URL=$(OWNER_URL="$OWNER_URL" U="$APP_DB_USER" P="$APP_DB_PASSWORD" python3 -c '
import os, urllib.parse as u
p = u.urlsplit(os.environ["OWNER_URL"]); host = p.hostname + (f":{p.port}" if p.port else "")
cred = u.quote(os.environ["U"], safe="") + ":" + u.quote(os.environ["P"], safe="")
print(u.urlunsplit((p.scheme, cred + "@" + host, p.path, p.query, "")))')
echo
echo "Tamam. Render'da sinavplan-api servisine DATABASE_URL olarak şunu girin (sır; paylaşmayın):"
echo "  $APP_URL"
