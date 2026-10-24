# Görev Durumu

Güncelleme: 2026-10-24 13:33 · Son teslim: 26 Ekim 2026 23.59 (`proje.md` §13)

## Tamamlanan
**Faz 0 — planlama:** gereksinim incelemesi · `docs/proje.md` ile görev tanımı farkı ·
7 gizli talimat tespit edilip reddedildi (ADR-000) · P0/P1/P2 sınıflandırması

**Faz 1 — iskelet (P0-1 ve P0-2 karşılandı):**
- `internal/config` (env, eksik alanlar tek seferde) · `internal/logging` (slog JSON + redaksiyon)
- `internal/httpapi`: ortak hata biçimi, chi router, RequestID/recover/metrics/timeout middleware,
  `/health` (DB ping + düşen bağımlılığın adı), `/metrics`
- `internal/store/db.go` (pgxpool) · `cmd/api` (SIGTERM'de 15 sn düzgün kapanış)
- `cmd/migrate`: **kısıtlı uygulama rolünü gerçekten oluşturuyor** (idempotent, `format(%I,%L)` ile güvenli)
- `backend/Dockerfile`: multi-stage, vendor'lı, ağsız derleme, root olmayan UID 10001 (ADR-002)
- `docker-compose.yml`: 7 servis, healthcheck'ler, `service_healthy`/`service_completed_successfully`
- `frontend/`: Vite+React+TS yer tutucusu — /health'i okuyup gösterir; yükleniyor/hata/sağlıksız durumları;
  nginx root olmayan kullanıcı, port 8080
- `Makefile` (26 hedef) · `.env.example` · `.github/workflows/ci.yml` (lint/test/docker/frontend) · `.golangci.yml`

## Sürmekte
Faz 2 — şema, migrasyonlar, sqlc, deterministik seed.

## Bloke
Yok.

## Yer tutucular (çalışıyormuş gibi gösterilmiyor; çıktıları "HENÜZ UYGULANMADI" diyor)
- `cmd/migrate`: rol oluşturma gerçek, **şema migrasyonları yok** → Faz 2
- `cmd/simulator`: argüman doğruluyor, **veri üretmiyor** → Faz 2
- `cmd/verify`: yalnızca HEALTH gerçek; 15 kimlik `YAPILMADI`, **exit 1** → Faz 9
- `frontend`: tek ekran (/health) → Faz 8.5

## Kalan P0
P0-3 şema+seed · P0-4 determinizm · P0-5 auth/RBAC · P0-6..P0-11 T-1..T-5 ·
P0-12 F-1..F-7 · P0-13 `make verify` · P0-14 test/CI (17/40 test) · P0-15 frontend

## Kalan P1
Idempotency · audit trigger · keyset · OpenAPI+`/docs` · k6 · chaos betiği ·
EXPLAIN · DECISIONS (3/5 ADR) · README · TESTING.md · reset-demo · simülatör `--live`

## Bilinen kusurlar / sınırlar
- **Soğuk süre bu ortamda ölçüldü** (66 sn); base imaj indirme süresi senin ağına bağlı, orada doğrulanmadı.
- Frontend Docker derlemesi bu sandbox'ta ancak proxy+CA ile mümkün; doğrulama gerçek Dockerfile'ın
  birebir kopyasıyla yapıldı (tek fark CA güveni, `diff` ile gösterildi). Normal ağda gerçek Dockerfile
  doğrudan çalışmalı — **bu senin makinende henüz denenmedi**.
- CI iş akışı yazıldı ama **GitHub'da henüz çalıştırılmadı** (ilk push sonrası görülecek).
- API açılışta DB'ye ulaşamazsa log'a pgx hatası yazılıyor (kullanıcı adı + iç IP içerir, **parola içermez**).
  İstemciye sızmıyor (test ediliyor). Yalnızca operatör log'u; kabul edilebilir.

## Son doğrulama sonuçları (2026-10-24 13:33)
```
go build / vet (-mod=vendor, Go 1.24)   OK
go test -race ./...                      OK — 17 test (config 7, httpapi 6, logging 4)
frontend: tsc --noEmit                   OK
frontend: vite build                     OK — 145 KB JS (gzip 47 KB)
docker compose config                    OK
soğuk derleme + başlatma                 66 sn (hedef <= 300)
/health sağlıklı                         200
/health postgres durdurulmuş             503 + "postgres": unhealthy
API çöktü mü                             hayır (running)
postgres geri gelince                    2. denemede kendiliğinden 200
postgres 5432 host'a açık mı             kapalı
süreç kullanıcıları                      api uid=10001, nginx=nginx (root değil)
make verify                              HEALTH PASS, 15 YAPILMADI, exit != 0 (doğru)
```

## Sıradaki eylem
Faz 2: `migrations/` (24 tablo + kısıtlar + dizinler + audit trigger + app rolü yetkileri),
goose embed, sqlc, `cmd/simulator seed` (deterministik, hash testi).
Model önerisi: **Sonnet 5.5** (rutin DDL + üretici), PROJECT_PLAN §7.
