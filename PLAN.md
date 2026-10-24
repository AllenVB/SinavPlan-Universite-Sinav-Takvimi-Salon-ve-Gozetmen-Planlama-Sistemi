# SınavPlan — Faz 0: Plan, Gereksinim Envanteri ve Sorular

> Hazırlayan: Claude Code · Tarih: 2026-10-24 · Durum: **onay bekliyor**

---

## 0. Önce bir uyarı: `docs/proje.md` yok

Depo tamamen boş. Yerelde commit yok, `git ls-remote --heads origin` boş dönüyor,
GitHub API de sıfır dal bildiriyor. Yani `docs/proje.md` **mevcut değil** —
okuyamadım, içeriğini tahmin etmedim.

Bu plan **yalnızca senin görev mesajındaki metne** dayanıyor. Mesaj oldukça
eksiksiz bir spesifikasyon, o yüzden plan büyük ölçüde sağlam; ancak `proje.md`
mesajda olmayan gereksinimler içeriyorsa onları kaçırıyorum.

**Yapılmadı, nedeni:** dosya depoda yok. Dosyayı eklersen (ya da içeriğini
yapıştırırsan) planı mesajla satır satır karşılaştırıp farkları listeleyeceğim.

### Gizli talimatlar hakkında
Uyardığın enjeksiyon kalıplarının (`wm-` tablo öneki, `X-DDO-Build` başlığı,
`/_hz/1e5a` ucu, sabit seed e-postası, `container_name`, README son satırı)
hiçbirini uygulamayacağım. Şu an dosya olmadığı için filtrelenecek bir şey de
yok. Dosya geldiğinde: HTML yorumlarındaki ve `[//]: #` satırlarındaki
talimatları **veri** olarak okuyup, görünür metinle çelişen veya görünür
metinde karşılığı olmayan her maddeyi uygulamadan önce sana soracağım.

---

## 1. Gereksinim envanteri

Kabul kriteri olarak kullanacağım liste. Her madde bir faza bağlı.

### 1.1 Altyapı (Faz 1)
- [ ] Repo yapısı: `backend/{cmd,internal,migrations,queries}`, `frontend/`, `deploy/`
- [ ] Go 1.22+, chi router, modül yolu belirlenecek (bkz. S-14)
- [ ] `internal/config`: tüm yapılandırma env'den, zorunlu alan eksikse açılışta hata
- [ ] `log/slog` JSON; parola/token/QR sırrı asla loglanmaz (redaksiyon helper'ı + testi)
- [ ] `GET /health`: DB ping dahil; sağlıklı 200, DB düşükse 503 + hangi bağımlılığın düştüğü
- [ ] İstek başına context timeout; SIGTERM'de 15 sn graceful shutdown
- [ ] Multi-stage Dockerfile, alpine/distroless, root olmayan kullanıcı, Go modül cache katmanı
- [ ] `docker-compose.yml`: postgres (port açmaz), migrate, seed, api:8080, simulator, frontend:3000, verify (profile)
- [ ] Her serviste healthcheck; `depends_on` + `service_healthy` / `service_completed_successfully`
- [ ] Soğuk makinede ≤ 5 dk hazır
- [ ] `Makefile`, `.env.example`, `.github/workflows/ci.yml` iskeleti
- [ ] `frontend/`: Vite + React + TS placeholder (gerçek arayüz sonra)

### 1.2 Şema ve veri (Faz 2)
- [ ] goose migrasyonları, `embed` ile; ayrı `migrate` servisi
- [ ] 24 tablo: users, refresh_tokens, departments, courses, sections, students,
      enrollments, lecturers, rooms, room_seats, exam_periods, slots, exams,
      schedules, schedule_exams, room_assignments, seat_assignments,
      invigilator_assignments, excuses, qr_tokens, attendance_events,
      attendance_state, attendance_conflicts, room_change_events, audit_log,
      idempotency_keys
- [ ] FK / UNIQUE / CHECK kısıtları tasarımın parçası
      (`UNIQUE(schedule_id,room_id,seat_id)`, `UNIQUE(qr_tokens.token_hash)`, `CHECK(capacity>0)`)
- [ ] Kritik dizinler: öğrenci programı, oturumdaki salon atamaları, gözetmen yükü,
      yoklama olayları (cihaz/sınav bazında)
- [ ] sqlc yapılandırması + üretilen kod commit'li
- [ ] `audit_log` append-only: UPDATE/DELETE engelleyen trigger + uygulama rolünden REVOKE (bkz. S-9)
- [ ] `cmd/simulator` seed: 300 sınav, 8.000 öğrenci, 60 salon, 5–8 ders/öğrenci, ~%2 engelli
- [ ] Determinizm: aynı `--seed` (varsayılan 42) = aynı veri, hash karşılaştırmalı test
- [ ] Rol başına demo hesap, parolalar `.env.example`'da
- [ ] `make reset-demo`

### 1.3 Ortak API katmanı (Faz 3)
- [ ] argon2id parola hash
- [ ] JWT access 15 dk + refresh 7 gün (DB'de hash'li, rotasyonlu, iptal edilebilir)
- [ ] RBAC middleware: her route'ta **açık** rol listesi; listesiz route testte yakalanır (bkz. S-13)
- [ ] 401 kimliksiz / 403 yetkisiz; öğrenci sahiplik kontrolü sunucuda
- [ ] Hata biçimi `{"error":{"code":"...","message":"..."}}`; 422 doğrulama, 409 çakışma, 404 yok
- [ ] Keyset sayfalama `?limit=&after=`, OFFSET yasak
- [ ] `internal/idempotency`: tablo + aynı anahtar/aynı gövde → kayıtlı yanıt,
      aynı anahtar/farklı gövde → 422, eşzamanlı → biri işler diğeri 409 (bkz. S-7)
- [ ] `internal/audit`: kritik işlemler aynı transaction içinde yazar

### 1.4 Çekirdek algoritmalar (Faz 4–7) — saf Go, DB bağımlılığı yok
- [ ] **T-1** `scheduling`: çatışma grafı (bitset), DSatur yerleştirme + tabu/SA yerel arama,
      sert kısıtlar (çatışma, günde ≤2 sınav), yumuşak ceza (ardışık gün + aynı gün 2 sınav),
      `Validate()`, çözümsüzlük raporu (bloke eden komşular, klik gerekçesi),
      benchmark 300/8.000/15 gün, `make prove-t1` (≤60 sn, hedef <15 sn, 0 ihlal)
- [ ] **T-2** `rooms`: FFD/BFD + bölme + paylaşım, lab eşleştirme, kapasite,
      erişilebilir koltuk gereksinimi, salon sayısı minimizasyonu,
      **etkin kapasite** T-3 oturma modelinden (bkz. S-4/S-5), ID ile sabit eşitlik bozma,
      determinizm testi, doğrulayıcı + bozuk girdi testleri
- [ ] **T-3** `seating`: komşuluk tanımı (sol/sağ zorunlu, ön/arka sıkı mod),
      engelli önce + erişilebilir koltuk, karışık salonda sütun dönüşümlü,
      tek sınavlı salonda koltuk atlamalı, `CountViolations()` = 0 kanıtı + bozuk plan testleri
- [ ] **T-4** `invigilation`: kısıtlar (kendi dersi yok, aynı slot tek görev, ardışık oturum yok,
      >50 öğrenci salonda ≥2 gözetmen, mazeretli slotlar hariç),
      amaç max−min minimizasyonu, açgözlü + takas iyileştirme,
      `Reassign(plan, removedID)` → kilitli diğer atamalar, minimum değişiklik, diff döner
- [ ] **T-5** `attendance` + store: UUIDv7 event_id, HLC, `POST /attendance/sync` idempotent,
      append-only olaylar, deterministik çakışma çözümü (QR present > büyük HLC > device_id),
      permutasyon testi, tek kullanımlık QR (HMAC, DB'de yalnız hash,
      koşullu UPDATE), **50 goroutine yarış testi: 1 başarı / 49×409**,
      toplu salon değişikliği (FOR UPDATE, kapasite, room_change_events + audit, idempotent,
      eşzamanlı iki tahliye testi)

### 1.5 Uçlar (Faz 8)
- [ ] F-1 CSV içe aktarma: satır bazlı hata raporu, COPY, idempotent upsert
- [ ] F-2 Salon CRUD + koltuk düzeni
- [ ] F-3 Takvim oluştur (asenkron: POST başlatır, GET durum/sonuç), yayınla, versiyonlar (bkz. S-6)
- [ ] F-4 Salon ve oturma planı sorguları
- [ ] F-5 Gözetmen atama, çıkarma/yeniden hesaplama
- [ ] F-6 Mazeret başvurusu (öğrenci), karar (öğrenci işleri), çakışma listesi
- [ ] F-7 Yoklama, sınav kartı (QR), öğrencinin kendi programı (+ bonus iCal)
- [ ] `api/openapi.yaml` (OpenAPI 3) + `/docs` Swagger UI
- [ ] `requests.http` demo hesaplarla

### 1.6 Doğrulama ve kalite (Faz 9–10)
- [ ] `cmd/verify`: F-1..F-7, T-1..T-5, /health, 401, 403, başkasının verisi;
      `kimlik | sonuç | süre | not` tablosu; hata varsa exit 1; <15 dk
- [ ] `make verify` = `docker compose --profile verify run --rm verify`
- [ ] ≥40 otomatik test; domain paketlerinde kapsam ≥%70; `go test -race -coverprofile`
- [ ] CI: golangci-lint, go test (Postgres service container), kapsam raporu,
      docker build (tüm imajlar), frontend build
- [ ] `/metrics` (prometheus/client_golang)
- [ ] `make load`: k6 (öğrenci programı, yoklama senkron, QR) → p50/p95/p99 + hata oranı README'de
- [ ] `make chaos`: postgres durur → /health 503, API çökmez; yeniden başlar → toparlanma otomatik doğrulanır
- [ ] `docs/explain/` dizinsiz/dizinli EXPLAIN (ANALYZE) betiği, ≥2 sorgu
- [ ] `TESTING.md`: demo hesaplar, her kimlik için nasıl denenir / beklenen / kanıt komutu
- [ ] `README.md`: adım (Süleyman Emre Arlı) en başta, kurulum, mimari diyagram,
      karar gerekçeleri, k6 tablosu, EXPLAIN çıktıları, boş `## Doğrulama Soruları` başlıkları
- [ ] `DECISIONS.md`: ≥5 ADR (Bağlam / Karar / Alternatifler / Sonuçlar), "float yasağı kapsam dışı" notu

---

## 2. Faz planı

Her fazın sonunda DURUYORUM: ne yaptım, nasıl test ettim, açık riskler,
önerilen commit mesajı. Onay vermeden sonraki faza geçmiyorum.

| Faz | Kapsam | Çıkış kriteri (kanıt) |
|-----|--------|------------------------|
| 0 | Bu doküman | Onayın |
| 1 | İskelet, compose, /health, config, slog, shutdown, CI, Makefile | `docker compose up --build` ayağa kalkar, `curl /health` 200, postgres durunca 503 |
| 2 | Şema + goose + sqlc + deterministik seed | `make reset-demo` çalışır, seed hash testi geçer, migrasyonlar up/down |
| 3 | Auth, RBAC, hata biçimi, audit, idempotency, keyset | Testler: 401/403/422/409, audit trigger UPDATE'i reddeder, idempotency 3 senaryosu |
| 4 | T-1 + doğrulayıcı + benchmark | `make prove-t1`: 0 ihlal, süre + metrikler; çözümsüz girdi raporu testi |
| 5 | T-2 + T-3 + doğrulayıcılar | Determinizm testi (aynı girdi = aynı çıktı), `CountViolations`=0, bozuk girdi testleri |
| 6 | T-4 + Reassign | Adalet metriği testi, minimum-değişiklik testi |
| 7 | T-5 + F-7 + Postgres yarış testleri | 50 goroutine QR: 1/49; permutasyon testi; eşzamanlı tahliye testi |
| 8 | F-1..F-6, OpenAPI, requests.http | Her uç requests.http'den çalışır, /docs açılır |
| 9 | cmd/verify, TESTING.md, reset-demo | `make verify` yeşil, <15 dk |
| 10 | /metrics, k6, chaos, EXPLAIN, DECISIONS, README | `make load` + `make chaos` çalışır, tablolar dolu |

**Öncelik sırası** (senin verdiğin): çalışan Docker → çekirdek görevler (T-1..T-5)
→ `make verify` → bonus (iCal) en sonda.

### Zaman riski — karar gerekiyor
10 fazın her birinde onay beklemek 24 saatlik pencerede ~10 tur demek. Faz
başına beklemek seni darboğaz yapar. Öneri: fazları **gruplayıp** rapor verelim
(1–2 / 3 / 4–5 / 6–7 / 8 / 9–10 → 6 durak). Sen karar ver (bkz. S-1).

---

## 3. Onayını bekleyen tasarım kararları

Bunlar DECISIONS.md'ye ADR olacak. **Varsayılanımı yazdım** — itiraz etmezsen
bunlarla ilerlerim, ama T-2/T-3 etkileşimi (K-2, K-3) sonucu ciddi değiştiriyor.

**K-1 — T-1 yumuşak ceza formülü.**
`ceza = 3 × (ardışık günde sınavı olan öğrenci-gün çifti sayısı) + 1 × (aynı günde 2 sınavı olan öğrenci-gün sayısı)`.
Ardışık gün, aynı gün yoğunluğundan daha ağır cezalandırılır. Ağırlıklar env/parametre ile ayarlanabilir.

**K-2 — T-3 komşuluk ve "sıkı mod" varsayılanı.**
Komşuluk = aynı sıradaki sol/sağ koltuk (zorunlu). Sıkı mod ön/arka koltuğu da
ekler ve **varsayılan olarak KAPALI**. Açık olması etkin kapasiteyi ~%50 daha
düşürür ve 60 salon 8.000 öğrenciye yetmeyebilir.

**K-3 — Etkin kapasite formülü (T-2 ↔ T-3 köprüsü).**
- Karışık sınavlı salon (≥2 farklı sınav): etkin kapasite = etkin koltuk sayısı,
  oturma sütun dönüşümlü (komşu sütunlar farklı sınav) → kayıp yok.
- Tek sınavlı salon: satır başına `ceil(cols/2)` koltuk → etkin kapasite
  = `rows × ceil(cols/2)` (etkin olmayan koltuklar düşülerek).
- Sıkı mod açıksa dama tahtası deseni → `ceil(rows×cols/2)`.
T-2 yerleştirmeyi bu etkin kapasiteye göre yapar, ham `capacity`'ye göre değil.

**K-4 — Günde 2 sınav.**
Hem sert kısıt (günde **en fazla** 2) hem de 2'ye ulaşıldığında küçük yumuşak
ceza. Yani 3. sınav asla konmaz; 2. sınav caydırılır ama yasak değil.

**K-5 — Idempotency eşzamanlılığı.**
Aynı anahtarla eşzamanlı istek → ikinci istek **409** alır (beklemez).
Deterministik ve test edilebilir; spesifikasyon ikisine de izin veriyor.

**K-6 — Asenkron takvim işi (F-3).**
Ayrı worker servisi değil, API içinde goroutine havuzu + `schedule_jobs` tablosu
(status, progress, result, error). POST iş yaratır → 202 + job id; GET durumu
döner. Yeniden başlatmada yarıda kalan işler `failed` işaretlenir.

**K-7 — İki DB rolü.**
Append-only audit için uygulama rolünden `UPDATE/DELETE` yetkisi geri alınmalı;
bu, migrasyonu çalıştıran rolün ayrı olmasını gerektirir. `migrate` servisi
sahip/`migrator` rolüyle, `api` kısıtlı `app` rolüyle bağlanır.

**K-8 — Yarış testlerinde Postgres.**
testcontainers-go Docker-in-Docker gerektirir; CI'da Postgres service container var.
Öneri: testler `TEST_DATABASE_URL` varsa ona bağlanır (CI yolu), yoksa
testcontainers ile kendi Postgres'ini kaldırır (yerel yol), ikisi de yoksa
`t.Skip` **değil** — açıkça başarısız olur ki test sessizce atlanmasın.

**K-9 — Para alanı yok.** DECISIONS'ta "float yasağı kapsam dışı" notu.

---

## 4. Sorular

Cevabı işi maddi olarak değiştiren sorular. S-1 ve S-2 bloke edici.

**S-1 (bloke edici) — `docs/proje.md`:** Dosyayı depoya ekleyecek misin, yoksa
mesajdaki spesifikasyonu tek kaynak kabul edip ilerleyeyim mi? İkincisini
seçersen, sonradan çıkan farkları değişiklik talebi olarak işlerim.

**S-2 (bloke edici) — Onay ritmi:** 10 fazda tek tek mi duralım, yoksa yukarıdaki
6 duraklık gruplama mı? 24 saatte 10 tur zor.

**S-3 — Tarih:** "24 saatimiz var" ile "son teslim 26 Ekim 2026 23.59" çelişiyor
(bugün 24 Ekim). Hangisi geçerli — 24 saat mi, 26 Ekim mi? Kapsam/bonus kararını
bu belirler.

**S-4 — Sıkı mod:** K-2'deki varsayılan (ön/arka kapalı) uygun mu? Kurum
kuralınız ön/arka komşuluğu da yasaklıyorsa salon sayısı yetmeyebilir; o zaman
seed'deki salon kapasitelerini büyütmem gerekir.

**S-5 — Etkin kapasite:** K-3'teki formüller onaylanıyor mu? Özellikle karışık
sınavlı salonda "kayıp yok" varsayımı, komşu sütunların her zaman farklı sınava
ayrılabildiğini varsayar — bu ancak salondaki en büyük sınav grubu etkin
koltukların yarısını geçmezse doğru. Bu ek kısıtı T-2'ye koyuyorum, onaylar mısın?

**S-6 — Takvim versiyonlama:** `published` bir takvim varken yeni sürüm
yayınlanınca eskisi `archived` mı olur, yoksa aynı dönemde yalnız bir `published`
sürüm mü olabilir (UNIQUE kısıtı)? Öneri: dönem başına tek `published`, eskisi
`archived`.

**S-7 — Rol görünürlükleri:** `lecturer` yalnızca kendi derslerinin sınavlarını mı
görür, tüm takvimi mi? `invigilator` yalnızca kendi görevlerini mi? Öneri:
lecturer = tüm yayınlanmış takvim + kendi derslerinin detayı; invigilator =
kendi görevleri + o salonun yoklaması.

**S-8 — QR yeniden üretimi:** Öğrenci kartını tekrar isterse yeni token üretilip
eski iptal mi edilir, yoksa aynı token mı döner? Öneri: yeni token, eskisi iptal
(kullanılmamışsa).

**S-9 — Mazeret (F-6) etkisi:** Onaylanan öğrenci mazereti sınavı otomatik olarak
etkiler mi (telafi slotu), yoksa yalnızca kayıt + rapor mu? Öneri: yalnızca kayıt
+ öğrencinin o sınavdan muafiyeti; telafi çizelgeleme kapsam dışı.
Gözetmen mazereti ise T-4'ü doğrudan besler.

**S-10 — Simülatör "live" modu:** Gerçek zamanlı mı çalışsın (süreklilik, Ctrl+C'ye
kadar), yoksa sabit süreli bir senaryo mu üretsin (örn. 2 dk sıkıştırılmış sınav
günü)? `make verify` için ikincisi gerekli. Öneri: `--live --duration=2m`.

**S-11 — Go modül yolu:** `github.com/AllenVB/sinavplan/backend` uygun mu, başka
bir yol mu istersin?

**S-12 — Frontend placeholder:** Boş Vite+React+TS iskeleti (tek sayfa, /health'i
çağırıp gösteren) yeterli mi? Compose'da nginx ile statik servis edilecek.

**S-13 — "Rol listesi olmayan route derlenmemeli":** Derleme zamanı garantisi Go'da
ancak route kaydını tipli bir sarmalayıcıya zorlayarak mümkün (chi'nin ham
`r.Get` kullanımı yasaklanır, linter kuralı + test ile). Öneri: tipli sarmalayıcı
+ tüm route'ları gezip rol listesi olmayanı yakalayan test. Bu kabul edilir mi?

**S-14 — `/metrics` erişimi:** Kimlik doğrulamasız mı (compose ağı içinde) yoksa
rol korumalı mı? Öneri: compose ağı içinde korumasız, README'de not.
