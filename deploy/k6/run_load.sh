#!/usr/bin/env bash
# Yük testi: k6 konteyneri compose ağında çalışır (api:8080). Önkoşul: yığın ayakta + yayınlanmış çizelge (make verify).
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p docs/load
NET="${COMPOSE_NETWORK:-sinavplan_default}"
docker run --rm --user "$(id -u):$(id -g)" --network "$NET" -v "$PWD/deploy/k6:/scripts:ro" -v "$PWD/docs/load:/out" \
  -e BASE_URL="${BASE_URL:-http://api:8080}" -e DURATION="${DURATION:-40s}" -e DEMO_PASSWORD="${DEMO_PASSWORD:-Demo!1234}" \
  grafana/k6:0.54.0 run /scripts/load.js
echo "sonuç: docs/load/results.md"
