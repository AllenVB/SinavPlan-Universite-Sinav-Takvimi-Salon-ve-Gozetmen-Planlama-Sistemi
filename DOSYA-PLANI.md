# Dosya Planı — Kurulacak Tüm Dosyalar

> Faz sırasına göre. ✅ = yazıldı · ⏳ = bu fazda · ⬜ = sonraki faz

## Faz 1 — İskelet
```
CLAUDE.md                                  ⏳ çalışma kuralları (kod standardı, test komutları, enjeksiyon reddi)
Makefile                                   ⏳ up/down/test/verify/load/chaos/prove-t1/reset-demo/explain
docker-compose.yml                         ⏳ postgres·migrate·seed·api·simulator·frontend·verify(profile)
.env.example                               ⏳ tüm env + rol başına demo hesap parolaları
.gitignore / .dockerignore                 ⏳
.github/workflows/ci.yml                   ⏳ lint·test(-race,coverage)·docker build·frontend build
backend/go.mod · go.sum                    ✅
backend/Dockerfile                         ⏳ multi-stage, alpine, root olmayan kullanıcı
backend/cmd/api/main.go                    ⏳ açılış, graceful shutdown (SIGTERM 15sn)
backend/internal/config/config.go          ✅ env'den yapılandırma, eksikse açılışta hata
backend/internal/config/config_test.go     ⏳
backend/internal/logging/logging.go        ✅ slog JSON + hassas alan redaksiyonu
backend/internal/logging/logging_test.go   ✅ 4 test — sızıntı kanıtı
backend/internal/httpapi/errors.go         ✅ {"error":{"code","message"}} + 401/403/404/409/422/503
backend/internal/httpapi/server.go         ✅ chi router, middleware zinciri
backend/internal/httpapi/health.go         ✅ /health — DB ping, düşen bağımlılığı bildirir
backend/internal/httpapi/metrics.go        ✅ /metrics — istek sayacı, süre histogramı, in-flight
backend/internal/httpapi/middleware.go     ⏳ RequestID·recoverPanic·observe·timeout
backend/internal/httpapi/health_test.go    ⏳ 200 / 503 testleri (sahte Pinger, DB gerekmez)
backend/internal/store/db.go               ✅ pgxpool havuzu + Ping
frontend/ (Vite+React+TS placeholder)      ⏳ Dockerfile·nginx.conf·package.json·src/
```

## Faz 2 — Şema ve seed
```
backend/migrations/0001_init.sql           ⬜ 24 tablo + FK/UNIQUE/CHECK
backend/migrations/0002_indexes.sql        ⬜ kritik dizinler (öğrenci programı, salon atamaları, gözetmen yükü, yoklama)
backend/migrations/0003_audit_append_only.sql ⬜ UPDATE/DELETE engelleyen trigger + app rolünden REVOKE
backend/migrations/embed.go                ⬜ goose embed
backend/cmd/migrate/main.go                ⬜ tek seferlik migrate servisi
sqlc.yaml · backend/queries/*.sql          ⬜ sqlc yapılandırması + sorgular
backend/internal/store/sqlc/*              ⬜ üretilen kod (commit'li)
backend/cmd/simulator/main.go              ⬜ --seed=42 deterministik üretici + --live modu
backend/cmd/simulator/generate.go          ⬜ 300 sınav·8000 öğrenci·60 salon·5-8 ders·~%2 engelli
backend/cmd/simulator/generate_test.go     ⬜ aynı seed = aynı veri (hash karşılaştırma)
```

## Faz 3 — Auth ve ortak katman
```
backend/internal/auth/{password,jwt,refresh}.go + testleri   ⬜ argon2id · JWT 15dk · refresh 7g rotasyonlu
backend/internal/httpapi/{auth_handlers,rbac}.go + testleri  ⬜ rol listesi zorunlu sarmalayıcı + listesiz route'u yakalayan test
backend/internal/httpapi/pagination.go + testi               ⬜ keyset (?limit=&after=), OFFSET yok
backend/internal/idempotency/{store,middleware}.go + testleri ⬜ aynı anahtar+gövde→kayıt · farklı gövde→422 · eşzamanlı→409
backend/internal/audit/audit.go + testi                      ⬜ aynı transaction içinde append-only yazım
```

## Faz 4-7 — Çekirdek görevler (saf Go domain paketleri, kapsam ≥%70)
```
internal/domain/scheduling/   graph.go·dsatur.go·localsearch.go·validate.go·report.go + testler + benchmark   ⬜ T-1
internal/domain/rooms/        capacity.go·assign.go·validate.go + testler (determinizm)                        ⬜ T-2
internal/domain/seating/      neighbors.go·plan.go·violations.go + testler                                     ⬜ T-3
internal/domain/invigilation/ assign.go·swap.go·reassign.go·validate.go + testler                              ⬜ T-4
internal/domain/attendance/   hlc.go·conflict.go + testler (permutasyon)                                       ⬜ T-5
internal/store/{attendance,qr,roomchange}_repo.go + yarış testleri (50 goroutine QR, eşzamanlı tahliye)        ⬜ T-5
backend/cmd/prove/main.go                                                                                      ⬜ make prove-t1
```

## Faz 8 / 8.5 — Uçlar ve arayüz
```
internal/httpapi/handlers_{import,rooms,schedules,seating,invigilation,excuses,attendance}.go + testler  ⬜ F-1..F-7
api/openapi.yaml · /docs Swagger UI · requests.http                                                      ⬜
frontend/src/ (giriş·takvim·salon+oturma·gözetmen·canlı yoklama·öğrenci programı+QR)                     ⬜ 6-7 ekran
```

## Faz 9-10 — Doğrulama ve doküman
```
backend/cmd/verify/main.go + checks/        ⬜ F-1..F-7·T-1..T-5·/health·401·403·sahiplik; tablo + exit 1
deploy/k6/{student_schedule,attendance_sync,qr_use}.js   ⬜ make load → p50/p95/p99
deploy/chaos/chaos.sh                       ⬜ postgres durur→503, geri gelir→toparlanma doğrulanır
deploy/explain/run_explain.sh               ⬜ docs/explain/ altına dizinsiz/dizinli EXPLAIN ANALYZE
TESTING.md · DECISIONS.md (≥5 ADR) · README.md  ⬜
```
