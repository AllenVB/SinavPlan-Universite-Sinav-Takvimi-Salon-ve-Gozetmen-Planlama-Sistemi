#!/usr/bin/env bash
# Kaos senaryosu: PostgreSQL durdurulur; API'nin (1) /health'te 503 verdiği, (2) veri uçlarında
# hızlıca hata döndürüp takılmadığı, (3) süreç yeniden başlatılmadan veritabanı dönünce toparlandığı doğrulanır.
# Başarısızlıkta çıkış kodu 1; her durumda postgres geri başlatılır.
set -uo pipefail
cd "$(dirname "$0")/../.."
API="${API_BASE_URL:-http://localhost:8080}"
DC="docker compose"
fail=0
ok()  { printf '  PASS  %s\n' "$1"; }
bad() { printf '  FAIL  %s\n' "$1"; fail=1; }
code() { curl -s -o /dev/null -m "${2:-10}" -w '%{http_code}' "$1" 2>/dev/null || echo 000; }
restore() { $DC start postgres >/dev/null 2>&1; }
trap restore EXIT

started_before=$($DC ps -q api | xargs docker inspect -f '{{.State.StartedAt}}')
echo "[1] başlangıç"
[ "$(code "$API/health")" = 200 ] && ok "/health 200 (bağımlılıklar sağlıklı)" || bad "/health başlangıçta 200 değil"
TOKEN=$(curl -s -m 10 -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"seed.mercan@ddo-case.invalid","password":"Demo!1234"}' | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')
[ -n "$TOKEN" ] && ok "giriş yapıldı" || bad "giriş yapılamadı"

echo "[2] postgres durduruluyor"
$DC stop postgres >/dev/null 2>&1
sleep 2
h=$(code "$API/health")
[ "$h" = 503 ] && ok "/health 503 (bağımlılık yok)" || bad "/health $h (503 bekleniyordu)"
t0=$(date +%s%N)
r=$(curl -s -o /dev/null -m 20 -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$API/rooms?limit=5" 2>/dev/null || echo 000)
ms=$(( ($(date +%s%N) - t0) / 1000000 ))
case "$r" in 503) ok "veri ucu hata döndü ($r) ${ms} ms içinde, takılmadı";; *) bad "veri ucu $r (503 bekleniyordu)";; esac
[ "$ms" -lt 15000 ] || bad "veri ucu ${ms} ms sürdü (zaman aşımı sınırı aşıldı)"
[ "$(code "$API/metrics")" = 200 ] && ok "/metrics çalışmaya devam ediyor" || bad "/metrics yanıt vermedi"
body=$(curl -s -m 10 -H "Authorization: Bearer $TOKEN" "$API/rooms?limit=5")
echo "$body" | grep -qiE 'password|dsn|postgres://|panic|goroutine' && bad "hata gövdesi iç ayrıntı sızdırıyor" || ok "hata gövdesinde iç ayrıntı yok"

echo "[3] postgres geri başlatılıyor"
$DC start postgres >/dev/null 2>&1
t0=$(date +%s)
rec=0
for i in $(seq 1 60); do
  [ "$(code "$API/health" 3)" = 200 ] && { rec=1; break; }
  sleep 1
done
[ "$rec" = 1 ] && ok "/health $(( $(date +%s) - t0 )) sn içinde 200'e döndü" || bad "60 sn içinde toparlanmadı"
r=$(code "$API/rooms?limit=5" 10)
[ "$r" = 401 ] && r=$(curl -s -o /dev/null -m 10 -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$API/rooms?limit=5")
[ "$r" = 200 ] && ok "veri ucu tekrar 200 (aynı access token geçerli)" || bad "veri ucu toparlanmadı ($r)"
started_after=$($DC ps -q api | xargs docker inspect -f '{{.State.StartedAt}}')
[ "$started_before" = "$started_after" ] && ok "API süreci yeniden başlatılmadı" || bad "API süreci yeniden başlatıldı"

[ "$fail" = 0 ] && echo "KAOS: GEÇTİ" || echo "KAOS: BAŞARISIZ"
exit $fail
