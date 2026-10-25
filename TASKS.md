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

**Faz 12–15 doğrulama:** `make verify` 23/23 PASS (yeni: L-1 canlı özet, Q-1 dinamik QR); `go test ./...` 171 test ok; UI duman: pages (tüm ekranlar), flows, features, live.cjs (canlı yenileme, dönen QR, kamera), qr-roundtrip. Bilinen: öğretim üyesi için ayrı ekran yok; Windows/CI doğrulanmadı; kamera yalnız sahte aygıtla sınandı (gerçek telefon kamerası denenmedi).

**Faz 16 doğrulama:** `make verify` 24/24 PASS (yeni FAK-1), `go test ./...` yeşil; seed özeti yenilendi; UI duman (pages/flows/features) PASS, F16 süzgeç ve kapsam testleri dahil. Bilinen: eski veritabanı için `docker compose down -v` gerekir; hoca havuzu yorumu ADR-016'da.

**Faz 17–18 doğrulama (bu oturum):** `go test ./...` yeşil; `make verify` 25/25 (yeni CHK-1, tam ölçek); UI duman `checkin.cjs` (gözetmen QR+kod → öğrenci kodla yoklamaya yazıldı → sayaç arttı) ve sekreterin mazeret onayı Vite üzerinden PASS. **Yapılmadı:** menü grupları / başlangıç rehberi / sekmeli takvim (dönem seçici de gereksiz kaldı: tek dönem), diğer duman betiklerinin (pages/flows/features/live) yeniden koşusu, `docs/screens` yenileme, gerçek telefon kamerası ile QR okuma, README'de Faz 17–18 bölümü, Windows/CI.

---

## Faz 19–21 planı (kalan işler; limit yenilenince buradan devam)

**Durum (25 Ekim 2026, ~14:30):** Son teslim 26 Ekim 23:59. Çekirdek T-1..T-5 ve F-1..F-7 tamam; CI (test/lint/frontend/docker) ilk kez yeşil; `make verify` 26/26. Aşağıdaki işler teslim kalitesini ve değerlendirme izlenimini artırır.

### Faz 19 — Teslim zorunlulukları (P0, ~3 sa)
| # | İş | Kabul ölçütü | Doğrulama |
|---|---|---|---|
| 19.1 | README/TESTING sayılarını güncelle: test sayısı (`go test ./... -v \| grep -c "^--- PASS"`), verify kimlik sayısı (26), ölçüm tabloları (T-1 ceza/süre, salon sayısı, gözetmen yükü yeni 301 sınavlı seed için), Faz 17–18 bölümleri, kapı + salon düzenleme ekranı | README'deki her sayı bir komutla yeniden üretilmiş; "178 test / 24 kimlik" kalmadı | `grep -n "178\|24 kimlik" README.md TESTING.md` boş |
| 19.2 | TESTING.md: CHK-1 (QR+kod yoklaması), F-2 kapı kontrolü, mazeret karar rolleri (sekreter + öğrenci işleri), `checkin.cjs` | Her yeni özellik için "nasıl denenir / beklenen" satırı | `make verify` çıktısındaki her kimlik TESTING.md'de var |
| 19.3 | `make load` (k6) ve `make explain` yeniden koş; README tablolarını yenile | p50/p95/p99 + hata oranı tablosu güncel; EXPLAIN çıktıları `docs/explain/` | k6 özeti, hata %0 |
| 19.4 | Duman betikleri (pages/flows/features/live/checkin/qr-roundtrip) yeniden koş; kırılan seçicileri düzelt | 6 betik PASS | betik çıktıları |
| 19.5 | `docs/screens` yenile: kapı krokisi, salon düzenleyici, QR+kod (gözetmen ve öğrenci), mazeret masası (sekreter), takvim süzgeçleri | README ekran tablosu yeni görsellerle | görseller depoda |
| 19.6 | ADR-018: salon kapısı + salon düzenleyici (Bağlam/Karar/Alternatifler/Sonuçlar) | DECISIONS.md'de ADR-018 | — |
| 19.7 | **Soğuk başlatma kanıtı:** CI'ya `docker compose up --build --wait` + `make verify` işi ekle (5 dk sınırı ölçülsün) | CI'da yeşil, süre özet adımında | Actions çıktısı |

### Faz 20 — Sade kullanım (P1, ~4 sa)
| # | İş | Kabul ölçütü |
|---|---|---|
| 20.1 | Yan menüyü gruplara ayır (Planlama / Sınav günü / Yönetim) | Her rolde ≤7 öğe, gruplar başlıklı |
| 20.2 | Ana panelde "Başlangıç rehberi": 4 adım, gerçek durumdan ✓ (çizelge var mı, yayınlandı mı, gözetmen atandı mı, canlı oturum var mı) | Adımlar veriyle ✓/○ değişir |
| 20.3 | Takvim sayfasını sekmelere böl: Takvim / Üret / Prova / Karşılaştır | Her sekme tek iş; mobilde taşma yok |
| 20.4 | "Varyasyon no" ve "Hesaplama kalitesi" → "Gelişmiş ayarlar" (kapalı başlar) | Varsayılan akış tek düğme |
| 20.5 | Takvim satırına "Öğretim üyesi" sütunu (API'de `lecturer_name` hazır) | Sütun görünür, aramada çalışır |
| 20.6 | Demo: demo sınavdan ÖNCEKİ oturumlara da yoklama ekle (panel sıfır yüzde göstermesin) | Panel/izleme ekranları ilk açılışta anlamlı |

### Faz 21 — İsteğe bağlı (P2, zaman kalırsa)
- Gerçek akademisyen adları (kullanıcı fakülte başına liste verirse `curriculum.go`'ya girilir; README'ye "adlar herkese açık listeden, ders atamaları kurgusal" notu).
- Vercel (frontend) + Render/Fly (API) + Neon (PostgreSQL) yayını: `frontend/vercel.json` (`/api` yönlendirmesi), `render.yaml`, uzak veritabanına migrate+seed betiği.
- Hatalı kod sayacını paylaşımlı depoya taşı (çok örnekli kurulum); öğrenci çevrimdışı yoklama kuyruğu.
- Erişilebilirlik taraması (klavye, odak, kontrast), karanlık tema.

### Kullanıcıdan beklenenler (benim yapamadıklarım)
1. **Doğrulama Soruları** son metni (özellikle 4. soru) — taslaklar README'de; sayılar Faz 19.1'de yenilenecek.
2. Depoyu `ddo-case-suleyman-emre-arli` adıyla **public** yap; bağlantıyı ankete yükle (26 Ekim 23:59'a kadar). Teslimden sonra force-push/özel yapma/silme **yok**.
3. İsteniyorsa gerçek hoca adları listesi.
4. Windows'ta soğuk kurulum denemesi (`git clone` → `copy .env.example .env` → `docker compose up --build`) ve süre.

### Bilinen sınırlar (README'de dürüstçe kalacak)
Commit yazarı Claude (sizin hesabınıza bağlı değil; karar: böyle kalsın); eski bir commit'te 9 MB derlenmiş ikili var (geçmiş yeniden yazılmadı); kamera ile QR okuma yalnız sahte aygıtla sınandı; hatalı kod sayacı tek örnek için bellekte.

### Önerilen sıra
19.1 → 19.2 → 19.6 → 19.3 → 19.4 → 19.5 → 19.7 → (zaman varsa) 20.1–20.6 → teslim kontrol listesi. Her adımdan sonra `go test ./...`, `make verify`, CI yeşil olmadan ilerleme.
