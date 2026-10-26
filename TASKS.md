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
Commit yazarı Claude (sizin hesabınıza bağlı değil; karar: böyle kalsın); eski bir commit'te 9 MB derlenmiş ikili var (geçmiş yeniden yazılmadı); kamera ile QR okuma yalnız sahte aygıtla sınandı; (hatalı kod sayacı Faz 21'de PostgreSQL'e taşındı).

### Önerilen sıra
19.1 → 19.2 → 19.6 → 19.3 → 19.4 → 19.5 → 19.7 → (zaman varsa) 20.1–20.6 → teslim kontrol listesi. Her adımdan sonra `go test ./...`, `make verify`, CI yeşil olmadan ilerleme.


## Durum güncellemesi (25 Ekim 2026, akşam)

**Tamam:** 19.1–19.7, 20.1–20.6. Faz 21 yapılmadı (zaman/istek).

| Doğrulama | Sonuç |
|---|---|
| `go test ./...` | 184 PASS, 0 FAIL (gerçek PostgreSQL) |
| `make verify` | 25 satır (26 kimlik) hepsi PASS |
| CI (main, 6372afd) | test, lint, frontend, docker, **soğuk kurulum** yeşil; compose up --build ≈ 70 sn, verify ≈ 25 sn (GitHub koşucusu) |
| UI duman | pages 17, flows 8, features 15, checkin 6, live 9, qr-roundtrip 5, simple 18 — hepsi PASS |
| k6 | 50.621 istek, hata %0, p95 70,2 ms, p99 106,3 ms |

**Doğrulanmayanlar:** Windows'ta soğuk kurulum; gerçek kamerayla QR okuma (yalnız sahte aygıt); Faz 20 sonrası CI koşusu push sonrası kontrol edilecek.
**Bilinen notlar:** `make verify` yayındaki çizelgeyi değiştirir → `checkin`/`live`/`screens` için önce `make reset-demo`; `checkin.cjs` aynı demo verisinde ikinci koşuda başarısız olur (öğrenci zaten yoklamada).

## Faz 21 durumu (25 Ekim 2026, gece)

| İş | Durum | Doğrulama |
|---|---|---|
| Hatalı kod sayacı → PostgreSQL (`checkin_failures`, migrasyon 0007) | tamam | `TestCheckinRateLimitIsSharedAcrossInstances` (iki API örneği, pencere sonu temizlik); mevcut kurulumda `migrate` 0007'yi veri silmeden uygular |
| Erişilebilirlik taraması (axe-core + klavye) | tamam | `a11y.cjs` 23/23 PASS, 20 ekranda 0 bulgu (önce: kontrast, adsız ilerleme çubuğu, odaklanamayan kaydırmalı bölge) |
| Vercel + Render + Neon yapılandırması | hazır, **yayınlanmadı** | yerelde Neon taklidi: migrate-seed.sh → production API `/health` healthy, giriş 200, kısıtlı rol audit_log UPDATE reddi |
| Gerçek akademisyen adları | **bekliyor** | kullanıcının fakülte başına ad listesi gerekli |
| Karanlık tema, çevrimdışı öğrenci yoklama kuyruğu | yapılmadı | P2, zaman/değer oranı düşük |

Son koşu: `go test ./...` 185 PASS / 0 FAIL; `make verify` 25 satır PASS; UI: simple 18, checkin 6, pages 17, flows 8, features 15, qr-roundtrip 5, a11y 23 PASS. `live.cjs` bu turda yeniden koşulmadı (`QR_TTL=20s` gerektirir; ilgili kod değişmedi).

## Faz 22 planı — "standart sınav provasından farklı" dokunuşlar (taslak; onay bekliyor)

**Neden:** Puanın 50'si yönetici incelemesi (çalışırken verdiği izlenim, kullanılabilirlik, kararların mantığı) ve 5'i 20 dk'lık görüşme. Çekirdek T-1..T-5 tamam; fark yaratacak şey, sistemin **gerçek bir sınav ofisinin işini kolaylaştırdığını** ve **neden böyle karar verdiğini gösterebilmesi**. İlke: her özellik (a) mevcut veriden beslenir, (b) varsayılan ekranı kalabalıklaştırmaz (Faz 20 sadeliği korunur), (c) test/verify ile kanıtlanır. Teslim 26 Ekim 23:59; kalan ~26 sa, ~6 sa güvenlik payı → en çok ~16 sa iş.

### A. Değerlendiriciyi yönlendiren deneyim (en yüksek getiri / en düşük risk)
| # | Özellik | Ne işe yarar | Efor | Kabul ölçütü |
|---|---|---|---|---|
| A1 | **Rehberli tur ("Demo senaryosu")**: ilk girişte/üst çubuktaki "Tur" düğmesiyle 6 adım: çizelge → neden bu slot → salon kapısı+koltuk → canlı yoklama → hasta gözetmen → mazeret; her adım ilgili ekrana götürür, hangi hesabı kullanacağını söyler | 20 dk'lık görüşmeyi ve yöneticinin ilk 5 dakikasını yönetir; hiçbir özellik gözden kaçmaz | 2 sa | 6 adımın hepsi gerçek ekranı açar; tur kapatılıp yeniden açılır; `simple.cjs`'e tur kontrolü |
| A2 | **"Kanıtlar" sayfası** (sekreter, Yönetim grubu): T-1..T-5 doğrulayıcılarını **canlı çalıştırır** (0 ihlal, süre, ceza), son `make verify` tablosu, EXPLAIN dizin farkı, test sayısı/kapsam | Zor görevlerin (40 puan) kanıtını arayüzde gösterir | 3 sa | Sayfadaki sayılar API'den hesaplanır (sabit metin yok); doğrulayıcı bozuk planda ihlal gösterir (test) |
| A3 | **Ctrl+K komut paleti + "Öğrenci 360"**: öğrenci no/ad, ders, salon ara → öğrencinin programı, salonu/koltuğu/kapısı, mazereti, yoklaması tek kartta | Ofisin en sık işi ("şu öğrenci nerede?") 2 saniyede | 3 sa | Rol yetkisi sunucuda denetlenir (öğrenci başkasını göremez, 403 testi); klavyeyle açılır/kapanır (a11y) |

### B. Planlama zekâsı (T-1..T-4'ün derinliğini görünür kılar)
| # | Özellik | Ne işe yarar | Efor | Kabul ölçütü |
|---|---|---|---|---|
| B1 | **Çakışma çözüm önerisi**: çakışan/yoğun öğrenci için "en iyi 3 taşıma" (ceza farkı + ihlal 0) tek tıkla taslak sürüm | Sekreter sorunu sadece görmez, çözer | 3 sa | Öneri uygulanınca ihlal 0 ve ceza ≤ önceki (test); çakışmaya yeni sürüm oluşur |
| B2 | **"Ya olursa?" önizleme (dry-run)**: gözetmen çıkar / salon kapanır / tahliye için **uygulamadan önce** etki özeti (kaç görev değişir, kim etkilenir, ihlal) ve "Uygula" | Kritik işlemde güven; F-5/T-5'in minimum-değişiklik iddiasını gösterir | 3 sa | Önizleme veritabanını değiştirmez (test: sürüm/atama sayısı aynı); uygula sonucu önizlemeyle aynı sayıları verir |
| B3 | **Rahatsızlık ısı haritası + "bu sınav neden burada?"**: gün × oturum yoğunluğu, öğrenci başına ardışık/aynı-gün dağılımı; sınav satırında kısıt gerekçesi ("12 ortak öğrenci olan 3 sınavla çakışmaz") | Çizelgenin kalitesini ve açıklanabilirliğini gösterir | 2 sa | Haritadaki toplamlar `metrics` ile birebir (test) |

### C. Sınav günü pratikleri
| # | Özellik | Ne işe yarar | Efor | Kabul ölçütü |
|---|---|---|---|---|
| C1 | **Yazdırılabilir föyler** (tarayıcı "PDF olarak kaydet"): salon kapısı listesi (öğrenci no + koltuk), gözetmen görev kâğıdı, imza föyü; kapı krokisi dahil | Gerçek sınav lojistiği; sunucuda PDF yok, yazdırma stili | 2 sa | Üç föy A4'te taşmadan yazdırılır (ekran görüntüsü); satır sayısı = oturan sayısı (test) |
| C2 | **Dikkat gerektirenler** (canlı ekranda otomatik uyarılar): oturum başladı + 15 dk, salonda geliş <%50; gözetmeni çıkarılmış/atanmamış salon; kapasite %95+ | Operasyonu proaktif yapar | 2 sa | Kurallar saf fonksiyon + birim test; demo verisinde en az bir uyarı görünür |
| C3 | **Tablet PWA**: kurulabilir (manifest + service worker), çevrimdışı kabuk; ağ yokken yoklama kuyruğu görünür | T-5'in çevrimdışı iddiasını gerçekten telefonda/tablette gösterir | 2 sa | Playwright `offline` modunda sayfa açılır, olay kuyruğa girer, ağ gelince senkron |

### D. İstenirse (P2)
D1 çözücü ilerleme grafiği (ceza eğrisi, canlı) · D2 öğrenci "Bugün" kartı + geri sayım + kapıdan koltuğa yönlendirme · D3 karanlık tema.

### Önerilen sıra ve bütçe (~16 sa)
A1 → B1 → C1 → A3 → A2 → B2 → C2 → B3 → C3 (zaman kalırsa D). Her özellikten sonra `go test ./...`, `make verify` (yeni kimlikler: **A2-1, A3-1, B1-1, B2-1, C2-1** TESTING.md'ye), duman testi, a11y taraması (0 bulgu korunur), CI yeşil. **Kesme noktası:** 26 Ekim 15:00'te biten ne varsa dondurulur; kalan süre README'nin son kontrolü, soğuk kurulum denemesi ve Doğrulama Soruları için ayrılır.

### Riskler
- Yeni uç noktalar RBAC testlerini bozabilir → her uç `Roles(...)` ile açılır, 403 testi eklenir.
- `make verify` 15 dk sınırı → yeni kontroller salt-okunur/dry-run, çizelge değiştirmez.
- Sadelik → hepsi mevcut menü altında (≤7 öğe korunur): Kanıtlar "Yönetim"e, tur ve arama üst çubuğa.

**Karar (25 Ekim, gece):** Faz 22 tamamı onaylandı; sıra A1 → B1 → C1 → A3 → A2 → B2 → C2 → B3 → C3. Her özellik bitince test + verify + duman + a11y çalıştırılır, GitHub'a (main + charming-archimedes-pszch2) commit atılır. Kesme noktası: 26 Ekim 15:00.
| Özellik | Durum |
|---|---|
| A1 Rehberli tur | **tamam** — 8 adım, rol değiştirme, `tour.cjs` 10/10 |
| B3 Isı haritası + neden burada? | **tamam** — `ExplainSlots` (doğrulayıcıyla tutarlılık testi), `/load` (SQL toplamı = çözücü metriği), `/why`; verify B3-1; `why.cjs` 6/6 (axe 0) |
| C2 Dikkat gerektirenler | **tamam** — 7 kural (saf fonksiyon + birim test), verify C2-1, `alerts.cjs` 2/2; "%95+ dolu" gürültü olduğu için kapasite aşımı alarmıyla değiştirildi, bitmiş oturum işaretsizleri tek özet |
| B2 "Ya olursa?" önizlemesi | **tamam** — `?dry_run=true` aynı işlemi koşup geri alır (gözetmen çıkarma + tahliye); verify B2-1; HTTP testi (veri özeti aynı, gerçek uygulama = önizleme); `preview.cjs` 8/8; `confirm()` diyalogları kaldırıldı |
| A2 Kanıtlar sayfası | **tamam** — 12 bağımsız SQL denetimi + bozulmuş sanal plan, T-5 canlı yazma reddi; verify A2-1; `proofs.cjs` 7/7. Bulunan hata: F-3 sonrası eski istatistikle >60 sn → `SET LOCAL enable_nestloop=off` + seed sonunda `ANALYZE` (1,96 sn) |
| A3 Ctrl+K + Öğrenci 360 | **tamam** — `/search`, `/students/{id}/overview`; verify A3-1; `palette.cjs` 12/12 (açıkken axe 0) |
| C1 Yazdırılabilir föyler | **tamam** — 3 föy, A4 sayfa bölme, gözetmen yalnız kendi görevleri; `sheets.cjs` 9/9 (PDF 7 sayfa doğrulandı, kroki bölünmüyor) |
| B1 Çakışma çözüm önerisi | **tamam** — öğrenci odaklı (çözücü çıktısı tek-hamlede yerel optimumda olduğundan "toplamı düşür" önerisi genelde boş: kaba kuvvetle doğrulandı); verify B1-1, `fix.cjs` 4/4, gerçek ölçek 0,16 sn |

### Tam test turu (25 Ekim 2026, ~22:50 UTC) — A1 + B1 sonrası, temiz kurulumdan (`docker compose down -v`)
| Doğrulama | Sonuç |
|---|---|
| `go test -race ./...` | 192 PASS, 0 FAIL, 1 SKIP (`TestFullScale` `-race` altında bilinçli atlanır; tam ölçek `prove-t1`'de) |
| Domain kapsamı | scheduling %95,4 · rooms %99,3 · seating %100 · invigilation %97 · attendance %100 |
| `make prove-all` | T-1..T-4 SONUÇ: BAŞARILI (0 ihlal) |
| `make verify` | 26 satır (27 kimlik, yeni **B1-1** dahil) hepsi PASS, 28 sn |
| UI duman | checkin 6, tour 10, simple 18, pages 17, flows 8, features 15, fix 4, qr-roundtrip 5, live 9 (`QR_TTL=20s`) — hepsi PASS |
| Erişilebilirlik | a11y 23/23, 20 ekranda axe bulgusu 0 |
| Frontend | `npm run build` başarılı (tsc + vite) |

**Yarın buradan devam:** C1 (yazdırılabilir föyler) → A3 (Ctrl+K + Öğrenci 360) → A2 (Kanıtlar sayfası) → B2 (ya olursa? önizleme) → C2 (dikkat gerektirenler) → B3 (ısı haritası) → C3 (PWA). Kesme noktası 26 Ekim 15:00; ardından README son kontrol, soğuk kurulum, Doğrulama Soruları.
Ortamı kaldırma: dockerd + `docker compose up -d --build postgres migrate seed api` + `cd frontend && npx vite --port 3000`; `checkin`/`live`/`tour` için taze seed (`make reset-demo` ya da `down -v`).
