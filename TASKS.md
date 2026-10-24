# Görev Durumu

Güncelleme: 2026-10-24 14:40 · Son teslim: 26 Ekim 2026 23.59 (`proje.md` §13)

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

**Faz 2 — şema + seed (P0-3, P0-4 karşılandı):**
- `migrations/`: 3 goose migrasyonu — 28 tablo, FK/UNIQUE/CHECK, 18 dizin, append-only trigger'lar (ADR-003, ADR-005)
- `internal/store`: `Migrate`, `EnsureAppRole`, `ApplyGrants`, `Seed` (cmd'ler ince, testler aynı kodu kullanır)
- sqlc v1.27 kuruldu; `queries/` + üretilen `internal/store/sqlc` commit'li
- `internal/simdata`: deterministik üretici (300 sınav / 8.000 öğrenci / 52.033 kayıt / 60 salon / 4.044 koltuk),
  `Hash()` + altın özet testi (ADR-004)
- `internal/auth/password.go`: argon2id (PHC biçimi, sabit zamanlı karşılaştırma, bellek bombası koruması)
- `cmd/simulator seed`: tek transaction + COPY, idempotent; `cmd/migrate [up|reset]`
- `make reset-demo` çalışıyor (6 sn); compose soğuk kurulumda seed otomatik

**Faz 3 — auth, RBAC, idempotency, audit, keyset (P0-5 karşılandı):**
- `internal/auth`: JWT HS256 (alg sabit, iss/exp zorunlu), access 15 dk; refresh 7 gün, DB'de SHA-256,
  rotasyonlu, yeniden kullanımda **aile iptali**, FOR UPDATE ile eşzamanlı yenilemede tek kazanan;
  zamanlama eşitlemeli giriş (kullanıcı numaralandırması yok)
- `internal/httpapi/rbac.go`: tipli route kaydı, Access sıfır değeri açılışta panik, Walk testi (ADR-006)
- `internal/idempotency` + middleware: replay / 422 / 409 / 5xx saklanmaz
- `internal/audit`: aynı transaction içinde yazım
- Keyset sayfalama (`?limit=&after=`, opak imleç, OFFSET yok)
- Uçlar: `POST /auth/login|refresh|logout`, `GET /me`, `GET /students`, `GET /students/{id}/exams`, `GET /me/exams`
- **Bulunup düzeltilen hata:** zaman aşımı middleware'inde panik süreci düşürüyordu + 2 veri yarışı (ADR-006)
- `make test-integration`: DB testleri konteynerde, compose ağında

## Sürmekte
Faz 4 — T-1 sınav takvimi çizelgeleme. Model: **Opus 5.5** (algoritma + performans).

## Bloke
Yok.

## Yer tutucular (çalışıyormuş gibi gösterilmiyor)
- `cmd/simulator live`: "HENÜZ UYGULANMADI" der (yoklama uçları Faz 7)
- `cmd/verify`: yalnızca HEALTH gerçek; 15 kimlik `YAPILMADI`, exit 1 (Faz 9)
- `frontend`: tek ekran (/health) (Faz 8.5)

## Kalan P0
P0-6..P0-11 T-1..T-5 ·
P0-12 F-1..F-7 · P0-13 `make verify` · P0-14 test/CI (67/40 test ✓; domain kapsamı henüz yok) · P0-15 frontend

## Kalan P1
OpenAPI+`/docs` · k6 · chaos betiği ·
EXPLAIN · DECISIONS (6 ADR: ≥5 sağlandı) · README · TESTING.md · reset-demo · simülatör `--live`

## Bilinen kusurlar / sınırlar
- **Soğuk süre bu ortamda ölçüldü** (66 sn); base imaj indirme süresi senin ağına bağlı, orada doğrulanmadı.
- Frontend Docker derlemesi bu sandbox'ta ancak proxy+CA ile mümkün; doğrulama gerçek Dockerfile'ın
  birebir kopyasıyla yapıldı (tek fark CA güveni, `diff` ile gösterildi). Normal ağda gerçek Dockerfile
  doğrudan çalışmalı — **bu senin makinende henüz denenmedi**.
- CI iş akışı yazıldı ama **GitHub'da henüz çalıştırılmadı** (ilk push sonrası görülecek).
- API açılışta DB'ye ulaşamazsa log'a pgx hatası yazılıyor (kullanıcı adı + iç IP içerir, **parola içermez**).
  İstemciye sızmıyor (test ediliyor). Yalnızca operatör log'u; kabul edilebilir.

## Son doğrulama sonuçları (Faz 3)
```
go test -race ./... (gerçek Postgres)     67 test geçiyor, veri yarışı yok (2 ardışık tur)
make test-integration                     konteyner içinde, 60 sn, tümü geçti
mutasyon kontrolü                         idempotency INSERT çakışması yok sayılınca 4 test,
                                          rol kontrolü kaldırılınca RBAC testleri BAŞARISIZ
çalışan yığında uçtan uca (curl)          yanlış parola 401 · token yok 401 · yetkisiz rol 403 ·
                                          başkasının verisi 403 · kendi programı 200 · keyset imleci
API log'unda parola/token                 0 geçiş
```
**Doğrulanmadı:** CI GitHub'da çalıştırılmadı; Windows makinende denenmedi.

## Sıradaki eylem
Faz 4: `internal/domain/scheduling` — bitset çatışma grafı, DSatur, tabu/SA yerel arama, Validate,
çözümsüzlük raporu, 300/8000/15 gün benchmark, `make prove-t1`.
