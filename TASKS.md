# Görev Durumu

Güncelleme: 2026-10-24 13:15 · Son teslim: 26 Ekim 2026 23.59 (`proje.md` §13)

## Tamamlanan
- Gereksinim incelemesi; `docs/proje.md` ile görev tanımı farkı çıkarıldı
- `docs/proje.md` içindeki **7 gizli talimat tespit edildi ve reddedildi** (DECISIONS ADR-000)
- Depo iskeleti: `backend/{cmd,internal,migrations,queries}`, `api/`, `deploy/`, `frontend/`
- Go modülü + bağımlılıklar (chi, pgx/v5, goose, prometheus, jwt, uuid, argon2)
- `internal/config` — env'den yapılandırma, eksik/geçersiz alanlar tek seferde bildirilir
- `internal/logging` — slog JSON + hassas alan redaksiyonu · **4 test geçiyor**
- `internal/httpapi/errors.go` — `{"error":{"code","message"}}` + 401/403/404/409/422/503
- `internal/httpapi/server.go` + `middleware.go` — RequestID·recover·metrics·timeout
- `internal/httpapi/health.go` — DB ping, düşen bağımlılığı adıyla bildirir
- `internal/httpapi/metrics.go` — istek sayacı, süre histogramı, in-flight
- `internal/store/db.go` — pgxpool havuzu + Ping

## Sürmekte (Faz 1)
- `cmd/api/main.go` — graceful shutdown (SIGTERM, 15 sn)
- `health_test.go` / `config_test.go`
- `Dockerfile` (multi-stage, root olmayan) · `docker-compose.yml` · `Makefile` · `.env.example`
- `.github/workflows/ci.yml` · frontend placeholder

## Bloke
Yok.

## Kalan P0
P0-3 şema+seed · P0-4 determinizm · P0-5 auth/RBAC · P0-6..P0-11 T-1..T-5 ·
P0-12 F-1..F-7 · P0-13 `make verify` · P0-14 test/CI · P0-15 frontend

## Kalan P1
Idempotency · audit trigger · keyset · `/metrics` uçtan uca · OpenAPI+`/docs` ·
k6 · chaos · EXPLAIN · DECISIONS ≥5 ADR · README · TESTING.md · reset-demo · simülatör `--live`

## Bilinen kusurlar
- `sqlc` ve `goose` CLI konteynerde kurulu değil → Faz 2'de `go install` ile kurulacak;
  kurulum başarısız olursa sqlc yerine elle yazılmış sorgu katmanına düşülecek (henüz denenmedi).
- `k6` kurulu değil → `make load` compose imajıyla çalışacak (henüz doğrulanmadı).

## Son doğrulama sonuçları
```
go build ./...                → OK
go test ./internal/logging/   → ok (4 test)
gofmt -l                      → temiz
```
Henüz çalıştırılmadı: `docker compose up`, migrasyonlar, uçtan uca akış.

## Sıradaki eylem
Faz 1'i bitir: `main.go` + health/config testleri + Dockerfile + compose + Makefile +
`.env.example` + CI + frontend placeholder → `docker compose up --build` ile
uçtan uca doğrula (P0-1, P0-2).
