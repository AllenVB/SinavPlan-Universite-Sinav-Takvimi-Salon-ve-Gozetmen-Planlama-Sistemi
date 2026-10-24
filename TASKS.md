# Görev Durumu

Güncelleme: 2026-10-24 16:50 · Son teslim: 26 Ekim 2026 23.59 (`proje.md` §13)

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

**Faz 4 — T-1 çizelgeleme (P0-6 karşılandı):**
- `internal/domain/scheduling`: bitset çatışma grafı, DSatur yapıcı + onarım, tohumlu SA (taşıma + Kempe),
  bağımsız `Validate`, çözümsüzlük raporu (engelleyenler, klik, aşırı yüklü öğrenci) — ADR-007
- `cmd/prove t1` / `make prove-t1`: 10,3 sn, 0 ihlal, ceza 16.851 → 8.407; CI'da da koşuyor
- 12 test + benchmark, kapsam **%95,1**; artımlı ceza = doğrulayıcı cezası testi

**Faz 5 — T-2 salon atama + T-3 oturma (P0-7, P0-8 karşılandı):**
- `internal/domain/seating`: sol/sağ komşuluk (ön/arka `Strict`, varsayılan kapalı), engelli öğrenciler önce
  erişilebilir koltuğa, sütun dönüşümlü / koltuk atlamalı, bağımsız `CountViolations` — kapsam %100
- `internal/domain/rooms`: etkin kapasite modeli, BFD + bölme + paylaşım + salon birleştirme, lab/klasik ayrımı,
  erişilebilir koltuk, kısmi yerleştirme yok, bağımsız `Validate`, girdi sırasından bağımsız deterministik — kapsam %97,9
- `internal/planning`: T-1 → T-2 → T-3 zinciri (Faz 8 servisi aynı dönüşümleri DB satırlarından yapacak)
- `make prove-t2` / `prove-t3` / `prove-all` (CI'da koşuyor)
- Seed düzeltmesi: erişilebilir koltuklar komşu değil (sütun 0,2,4), labların yarısında da var (ADR-004 özeti yenilendi)
- **Bulunan ve düzeltilen hata:** T-2 doğrulayıcısı 0 ihlal derken T-3 110 öğrenciyi açıkta bırakıyordu (kural iyimserdi) — ADR-008

**Faz 6 — T-4 adil gözetmen atama (P0-9 karşılandı):**
- `internal/domain/invigilation`: kısıtlar (kendi dersi, aynı slot, ardışık oturum, >50 → 2, mazeret),
  açgözlü + artırma zinciri onarımı + takas iyileştirme, `Reassign` (kilitli, iteratif derinleştirme, diff),
  bağımsız `Validate`, `Fairness` — kapsam %97 (ADR-009)
- `planning.InvigilationInput`; `make prove-t4`
- **Bulunan zayıflık:** açgözlü atama çözülebilir bir günde 3 kontenjanı boş bırakıyordu → onarım eklendi

## Sürmekte
Faz 7 — T-5 yoklama/çevrimdışı senkron + F-7 + Postgres yarış testleri. Model: **Opus 5.5** (eşzamanlılık).

## Bloke
Yok.

## Yer tutucular (çalışıyormuş gibi gösterilmiyor)
- `cmd/simulator live`: "HENÜZ UYGULANMADI" der (yoklama uçları Faz 7)
- `cmd/verify`: yalnızca HEALTH gerçek; 15 kimlik `YAPILMADI`, exit 1 (Faz 9)
- `frontend`: tek ekran (/health) (Faz 8.5)

## Kalan P0
P0-10..P0-11 T-5 ·
P0-12 F-1..F-7 · P0-13 `make verify` · P0-14 test/CI (122/40 test ✓; domain kapsamı: scheduling %96,1 · rooms %97,9 · seating %100 · invigilation %97) · P0-15 frontend

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

## Son doğrulama sonuçları (Faz 6)
```
make prove-t4   674 görev · 1.092 atama · 0 eksik · 0 ihlal · yük 10–11 (fark 1 = ideal) · 6 ms
                gözetmen çıkarma: 11 boşalan -> 11 değişiklik (alt sınır) · 1.081 atama dokunulmadı · 0 ihlal
birim testler   imkânsız-doğrudan senaryoda tam 1 takas (2 değişiklik), kilitli atamalar değişmedi
go test -race   invigilation geçti, kapsam %97
```
**Doğrulanmadı:** CI GitHub'da çalıştırılmadı; Windows'ta denenmedi.

## Sıradaki eylem
Faz 7: `internal/domain/attendance` (HLC, çakışma kuralı, permutasyon testi), store'da senkron (event_id UNIQUE),
tek kullanımlık QR (HMAC, hash, koşullu UPDATE, 50 goroutine yarış testi), toplu salon değişikliği (FOR UPDATE,
audit, idempotency, eşzamanlı tahliye testi), F-7 uçları.
