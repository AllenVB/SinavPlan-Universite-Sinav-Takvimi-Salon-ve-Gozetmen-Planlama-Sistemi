# Görev Durumu

Güncelleme: 2026-10-24 19:20 · Son teslim: 26 Ekim 2026 23.59 (`proje.md` §13)

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

**Faz 7 — T-5 yoklama + F-7 (P0-10, P0-11 karşılandı):**
- `internal/domain/attendance`: HLC saati, tam sıra çakışma kuralı, `Resolve` (permütasyondan bağımsız) — kapsam %100
- `internal/store`: `SyncAttendance` (UUIDv7, idempotent, advisory lock, olaylardan yeniden hesaplanan durum),
  `QR` (HMAC, yalnız özet, koşullu UPDATE, yeniden üretimde iptal), `RoomChange` (FOR UPDATE, ArrangeInto, audit),
  `CreateSchedule`/`PublishSchedule` (sürümlü, COPY, audit)
- Uçlar: `POST /me/exams/{examId}/card`, `POST /attendance/sync` (idempotent), `POST /attendance/qr` (idempotent),
  `GET /exams/{id}/attendance`, `GET /attendance/conflicts`, `POST /schedules/{id}/room-change` (idempotent)
- `cmd/simulator live`: çevrimdışı tabletler, toplu gönderim, bilerek tekrar ve çakışma
- `make prove-t5` — ADR-010 (testcontainers-go yerine TEST_DATABASE_URL: bilinçli sapma)

**Faz 8 — F-1..F-6 + OpenAPI (P0-12 karşılandı):**
- F-1 `POST /imports/{students|courses|sections|enrollments}` (COPY + upsert, satır bazlı hata, idempotent)
- F-2 salon CRUD + koltuk düzeni (`/rooms`; kullanılan salonun düzeni değişirse 409)
- F-3 `POST /schedules` (202, asenkron iş) · `/schedule-jobs/{id}` · sürümler · yayınla (idempotent) · takvim
- F-4 `/schedules/{id}/rooms` ve `/seating` (öğretim üyesi yalnız kendi dersi, gözetmen yalnız kendi salonu)
- F-5 görev listesi, gözetmen çıkarma = `Reassign` (idempotent), `/me/duties`
- F-6 mazeret başvuru/karar (idempotent), `/excuses`, `/me/excuses`, `/schedules/{id}/conflicts`, yoklama çakışmaları
- `backend/api/openapi.yaml` (44 uç) + gömülü Swagger UI `/docs` (çevrimdışı) + **belge–kod uyumu testi**
- `requests.http`: 5 rol, tüm uçlar, değişken zincirleri (ADR-011)

## Sürmekte
Faz 9, 10 ve 8.5 TAMAM. Kalan: P2 (iCal, plan karşılaştırma), CI'nın GitHub'da doğrulanması, teslim kontrol listesi.

## Bloke
Yok.

## Yer tutucular (çalışıyormuş gibi gösterilmiyor)
- `cmd/verify`: 16 kimlik gerçek, `make verify` ~30 sn, hepsi PASS (iki ardışık koşu); `TESTING.md` yazıldı
- `frontend`: tek ekran (/health) (Faz 8.5)

## Kalan P0
P0-13 `make verify` (15 kimlik YAPILMADI) · P0-15 frontend (Stitch tasarımı bekleniyor) ·
P0-14 CI'nın GitHub'da gerçekten yeşil çalıştığının doğrulanması (155/40 test ✓; domain kapsamı: scheduling %96,1 ·
rooms %97,9 · seating %100 · invigilation %97 · attendance %100)

## Kalan P1
`TESTING.md` · `make load` (k6) · `make chaos` · EXPLAIN betiği (`docs/explain/`) · README (+ Doğrulama Soruları) ·
`/metrics` zaten çalışıyor (ölçüm tablosu README'de) · DECISIONS ≥5 ADR sağlandı (011)

## Bilinen kusurlar / sınırlar
- **Soğuk süre bu ortamda ölçüldü** (66 sn); base imaj indirme süresi senin ağına bağlı, orada doğrulanmadı.
- Frontend Docker derlemesi bu sandbox'ta ancak proxy+CA ile mümkün; doğrulama gerçek Dockerfile'ın
  birebir kopyasıyla yapıldı (tek fark CA güveni, `diff` ile gösterildi). Normal ağda gerçek Dockerfile
  doğrudan çalışmalı — **bu senin makinende henüz denenmedi**.
- CI iş akışı yazıldı ama **GitHub'da henüz çalıştırılmadı** (ilk push sonrası görülecek).
- API açılışta DB'ye ulaşamazsa log'a pgx hatası yazılıyor (kullanıcı adı + iç IP içerir, **parola içermez**).
  İstemciye sızmıyor (test ediliyor). Yalnızca operatör log'u; kabul edilebilir.

## Son doğrulama sonuçları (Faz 8)
```
go test ./... (gerçek Postgres)   155 test geçti, 0 başarısız; -race: 0 sorun
çalışan yığın, tam ölçek          POST /schedules -> 202; iş 12 sn; 0 salon/koltuk ihlali; gözetmen yükü 10-11; yayınlandı;
                                  demo öğrenci programı salon + koltukla dönüyor
OpenAPI                           44 uç belgelenmiş = 44 kayıtlı; uç silinince test düşüyor (mutasyon)
/docs                             çevrimdışı (1,4 MB Swagger UI gömülü), CDN yok
çözümsüz girdi                    iş failed + engelleyen sınavlar/neden; schedules tablosu boş kalıyor
```
**Doğrulanmadı:** CI GitHub'da çalıştırılmadı; Windows'ta denenmedi; frontend tasarımları alınmadı.
**Olay notu:** bir kabuk komutunda tırnaksız heredoc yüzünden `make reset-demo` istemeden çalıştı; demo veritabanı seed
haline döndü (üretilmiş demo çizelgesi silindi, yeniden üretilebilir). Kaynak dosyalar etkilenmedi.

## Sıradaki eylem
Faz 10 TAMAM (k6, chaos, EXPLAIN, README, ADR-012). Tasarımlar push edilince Faz 8.5 (frontend entegrasyonu).

**Faz 9 doğrulama:** `make verify` iki ardışık koşuda 16/16 PASS; `go test ./...` hepsi ok. Verify gerçek bir hata yakaladı
(`/schedules/{id}/conflicts` keyset imleci aynı öğrencinin ikinci gününü atlıyordu) ve düzeltildi (TESTING.md).

**Faz 10 doğrulama:** `make load` 47.612 istek, hata %0, p95 63 ms; `make chaos` GEÇTİ (503, ~1 sn toparlanma); `make explain` 4 sorgu. Kaos, veri uçlarının 500 döndüğünü yakaladı → 503'e düzeltildi.

**Faz 8.5 doğrulama:** 11 ekran, 5 rol; Playwright: 16/16 ekran hatasız + 8/8 akış PASS (Vite üzerinden); nginx /api vekili ayrı konteynerde doğrulandı; `docker compose build frontend` bu ortamda npm ci ağ erişimi yüzünden çalışmadı (kullanıcı makinesinde denenmeli).

**Faz 11 doğrulama:** X-1..X-7 tamam (ÖBS kapsam dışı, ADR-014). `make verify` 21/21 PASS (tam ölçek), `go test ./...` 165 test ok, mutasyonla taşıma ihlal denetiminin testi doğrulandı, UI duman: QR/indirme/simülasyon/akış/gerçek sürükle-bırak PASS.
