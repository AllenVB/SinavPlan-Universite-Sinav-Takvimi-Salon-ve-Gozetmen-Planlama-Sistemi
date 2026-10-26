# TESTING.md — Nasıl doğrulanır

Her gereksinim kimliği için: **nasıl denenir**, **beklenen sonuç**, **kanıt komutu**. Hepsi tek komutla
çalışır: `make verify` (yığın ayakta olmalı: `cp .env.example .env && docker compose up --build -d`).
`make verify`, sistemi **yalnızca HTTP API üzerinden** dener (çözücü koduna/DB'ye dokunmaz); T-1..T-4 kısıtları API'den
okunan veriden bağımsız yeniden hesaplanır. PASS olmayan kimlik varsa çıkış kodu 1'dir. Süre: ~30 sn.

Demo hesapları (parola `Demo!1234`, yalnızca yerel demo): `sekreter@`, `hoca@`, `gozetmen@`, `ogrenciisleri@`,
`ogrenci@` + `sinavplan.test`. Elle deneme: `requests.http` (VS Code REST Client) veya `http://localhost:8080/docs`.

## Kimlik tablosu

| Kimlik | Nasıl denenir | Beklenen | Kanıt komutu |
|---|---|---|---|
| HEALTH | `GET /health` | 200, `postgres: healthy`; `/metrics`, `/docs`, `/openapi.yaml` 200 | `make verify` (HEALTH) |
| AUTH-401 | yanlış parola, bilinmeyen kullanıcı, token yok/bozuk, kullanılmış refresh | 401; iki 401 gövdesi aynı; yeniden kullanımda zincir iptal | `make verify` (AUTH-401) |
| AUTH-403 | yanlış rolle 7 uç (öğrenci→`/students`, hoca→`POST /rooms`, …) | 403 | `make verify` (AUTH-403) |
| F-1 | `POST /imports/{kind}` (CSV) | satır hataları satır numarasıyla; tekrar yükleme `created=0`; başlık 422; bilinmeyen tür 404 | `make verify` (F-1) |
| F-2 | salon CRUD, koltuk düzeni, **salon kapısı** (`door_side` front/back/left/right + `door_pos`), lab filtresi + keyset | kapasite = satır×sütun−pasif; kapı `right/3` aynen geri okunur; geçersiz kapı (konum salon dışı: `front` + pos 5, 2×2 salonda) 422; yinelenen 409 | `make verify` (F-2) |
| F-3 | `POST /schedules` (202) → iş sorgula → yayınla | 2. eşzamanlı istek 409; aynı tohum → özdeş çizelge; yayın `Idempotency-Key` ile idempotent | `make verify` (F-3) |
| T-1 | API'den takvim | hiçbir öğrenci aynı slotta 2 sınav / günde >2 sınav yok; sert ihlal 0 | `make verify` (T-1) · `make prove-t1` |
| T-2 | slot–salon payları | kapasite aşılmaz, lab/klasik uyumlu, paylar oturma planıyla birebir | `make verify` (T-2) · `make prove-t2` |
| T-3 | oturma planı | yan yana aynı sınav yok, koltuk tekil, engelli öğrenci erişilebilir koltukta | `make verify` (T-3) · `make prove-t3` |
| F-4 | `/me/exams`, `/schedules/{id}/rooms`, `/seating` | öğrencinin ekranındaki salon+koltuk planla aynı | `make verify` (F-4) |
| T-4 | gözetmen görevleri | aynı slotta tek görev, ardışık oturum yok, >50 öğrenci→2 gözetmen, yük farkı ≤2 | `make verify` (T-4) · `make prove-t4` |
| F-5 | `POST /schedules/{id}/invigilators/{u}/remove` | ihlal 0, değişiklik = boşalan + takas, kalan atamalar kilitli, yanıt idempotent | `make verify` (F-5) |
| F-6 | mazeret oluştur (öğrenci/gözetmen), **karar: yalnız sekreter ve öğrenci işleri**, çakışma listesi | yinelenen 409, ikinci karar 409, onaylı mazeretli sınavın kartı 409 `excused`; gözetmen mazereti reddi; karar rolü olmayan 403 | `make verify` (F-6) · `AUTH-403` |
| CHK-1 | gözetmen `POST /checkin-sessions` (QR + 6 haneli kod) → öğrenci `POST /me/checkin` | doğru kod → yoklamada `geldi`; yanlış kod 422; eski (döndürülmüş) kod 422; kapalı oturum 409; öğrenci oturum açamaz 403; salon eşleşmesi ve onaylı mazeret denetlenir (ADR-017) | `make verify` (CHK-1) |
| OWNERSHIP | başkasının verisi | öğrenci 403/404; hoca yalnız kendi dersi; gözetmen yalnız kendi salonundaki sınav; taslak 404 | `make verify` (OWNERSHIP) |
| F-7 | kart → QR → senkron | QR 200, ikinci okutma 409 `qr_already_used`; sync tekrarı `duplicates`; QR≻elle-absent çakışması `qr_present` | `make verify` (F-7) |
| T-5 | 20 eşzamanlı okutma; tahliye | tam 1×200 + 19×409; yetersiz kapasite 409 ve veri değişmez; başarılı tahliye sonrası T-3/T-4 ihlali 0 | `make verify` (T-5) · `make prove-t5` |

| X-2 | `GET /schedules/{id}/export?format=xlsx\|csv` | geçerli zip (2 sayfa); csv BOM'lu, satır = sınav sayısı; geçersiz biçim 422 | `make verify` (X-2) |
| X-3 | `calendar.ics`, `/me/exams.ics`, `/me/duties.ics` | VEVENT sayısı = veri; öğrenci `.ics` salon+koltuk içerir; CRLF | `make verify` (X-3) |
| X-4 | `POST /schedules/simulate` | `saved=false`, sürüm sayısı değişmez, aynı girdi aynı ceza | `make verify` (X-4) |
| X-5/X-7 | `POST /schedules/{id}/exams/{e}/move`, `GET /schedules/compare` | çakışan taşıma 422 (sürüm yok); geçerli taşıma yeni taslak, ihlal 0, yalnız 1 sınav farklı; karşılaştırma 1 | `make verify` (X-5/X-7) |
| X-6 | `GET /activity` | denetim kaydından, yeniden eskiye; öğrenci/hoca/gözetmen 403 | `make verify` (X-6) |
| FAK-1 | `GET /faculties`, takvim satırları, `POST /schedules/simulate` + `scope` | 4 fakülte/15 bölüm (her biri 4 sınıf × 5 ders), mühendislik 8 adlı öğretim üyesi, diğerleri 4–6; prova kapsamı 300→60→20→5 sınav; geçersiz kapsam 422 | `make verify` (FAK-1) |
| L-1 | `GET /schedules/{id}/live`, `/live/events`, `/activity?after_id=` | oturan = oturma planı; gelen/gelmedi = sınav bazlı yoklama uçlarının toplamı; her salonda gözetmen; roller 403 | `make verify` (L-1) |
| Q-1 | `POST /me/exams/{id}/card` ×2 | jeton ömrü ≤ QR_TTL; yenileme eski jetonu iptal eder (409 `qr_expired`), güncel 200 | `make verify` (Q-1) |
| UI-SADE | menü ≤7 öğe + gruplar, başlangıç rehberi, takvim sekmeleri, Gelişmiş ayarlar kapalı, Öğretim üyesi sütunu/arama | 18 kontrol PASS | `NODE_PATH=$(npm root -g) node deploy/ui-smoke/simple.cjs` |
| A11Y | axe-core WCAG 2.1 A/AA, 5 rolün 20 ekranı + klavyeyle giriş + odak halkası | serious/critical bulgu 0 (son koşu: hiç bulgu yok), 23 kontrol PASS | `NODE_PATH=$(npm root -g) AXE_PATH=…/axe.min.js node deploy/ui-smoke/a11y.cjs` |
| CHK-RL | 8 hatalı koddan sonra 429; ikinci API örneği de engelli; 5 dk sonra engel kalkar | `TestCheckinCodeGuessingIsRateLimited`, `TestCheckinRateLimitIsSharedAcrossInstances` | `go test ./internal/httpapi -run Checkin` |
| TUR | üst çubuk → "Tur": 8 adım gerçek ekranları açar; başka rolün adımında "Bu hesapla devam et" | adım 2 `#/takvim?tab=uret`, adım 6'da gözetmene geçip 6/8'den sürer, Esc kapatır, mobilde taşmaz, açıkken a11y 0 | `NODE_PATH=$(npm root -g) node deploy/ui-smoke/tour.cjs` |
| B1-1 | `GET /schedules/{id}/fix-suggestions?student_id=` (salt-okur) | her öneri API takviminden bağımsız yeniden denetlenir: slot çakışması 0, günde ≤2 sınav, sorunlu öğrencinin hedef günü boş, başlamış oturuma dokunmaz; sürüm sayısı değişmez; öğrenci 403 | `make verify` (B1-1) · `go test ./internal/domain/scheduling -run Suggest` · `deploy/ui-smoke/fix.cjs` |
| FÖY | Salon ve oturma planı → Föyler → oturum + salon → Yazdır | kapı listesi ve imza föyü satırı = salonda oturan; numaraya göre sıralı; kapılı kroki; baskıda menü/denetimler gizli; PDF ≥3 sayfa; gözetmen yalnız kendi görevleri; a11y 0 | `NODE_PATH=$(npm root -g) node deploy/ui-smoke/sheets.cjs` |
| A3-1 | `GET /search?q=`, `GET /students/{id}/overview` | arama demo öğrenciyi numara önekiyle bulur; `%_%` → 0 sonuç; 360'taki her sınavın salon+koltuğu oturma planıyla aynı; öğrenci başkasının 360'ı ve arama → 403 | `make verify` (A3-1) · `go test ./internal/httpapi -run SearchAndStudent` · `deploy/ui-smoke/palette.cjs` |
| X-1 | öğrenci kartı | tarayıcıda gerçek QR (SVG) | `deploy/ui-smoke/features.cjs` |
| UI-CHK | tarayıcıda yoklama: gözetmen "Yeni QR oluştur" → öğrenci kodu girer | gözetmen sayacı artar; yanlış kod hata verir | `NODE_PATH=$(npm root -g) node deploy/ui-smoke/checkin.cjs` (demo verisi ve süren oturum gerekir) |

Birim/entegrasyon testleri (gerçek PostgreSQL ister): `make test` (yarış dedektörüyle) ya da
`TEST_DATABASE_URL=… go test ./...`. Etki alanı paketleri deterministik/saf Go'dur; DB'siz çalışanlar için `make test-short`.

## Verify'ın yakaladığı gerçek hata

İlk koşuda T-1, `GET /schedules/{id}/conflicts` uç noktasının 9.165 satır (o günkü veri kümesinde) döndürdüğünü, bağımsız hesabın 9.180 çıkardığını
gösterdi: keyset imleci yalnız `student_id` olduğu için aynı öğrencinin sayfa sınırına denk gelen ikinci günü atlanıyordu.
İmleç `(öğrenci, gün)` çiftine çevrildi (`store.ConflictCursor`); aynı kontrol artık 9.180 = 9.180 ile geçiyor.

## Elle UI senaryoları (http://localhost:3000, demo hesapları yukarıda)

| Senaryo | Adımlar | Beklenen |
|---|---|---|
| Giriş / hata | yanlış parola gir | kırmızı hata kutusu; doğru parolayla rolün ana ekranı |
| Takvim üret | sekreter → Sınav takvimi → "Takvimi oluştur" | durum Sırada→Çalışıyor→Tamamlandı, yeni sürüm listede; ikinci tıklama çalışırken kapalı |
| Yayınla | taslak sürümde "Bu sürümü yayınla" | sürüm "yayında"; öğrenci/öğretim üyesi artık görür |
| Oturma planı | Salon ve oturma planı → sınav seç | salon başına koltuk haritası, engelli öğrenci yeşil, yan yana aynı sınav yok |
| Tahliye | aynı ekranda kaynak/hedef salon + gerekçe | taşınan sayısı; sığmazsa kırmızı uyarı ve hiçbir şey değişmez |
| Gözetmen çıkar | Gözetmen atama → "Hasta / çıkar" | boşalan/yeniden atanan/takas özeti, ihlal 0 |
| İçe aktarma | Veri içe aktarma → hatalı CSV | satır numaralı hata tablosu |
| Mazeret | öğrenci: mazeret gönder → öğrenci işleri: onayla | onaylanan sınavın kartı "mazeretiniz var" uyarısı verir |
| Yoklama | gözetmen: görev seç → Geldi → Senkronla | "1 senkron bekliyor" → "kabul edildi"; çevrimdışıyken kuyruk kalır |
| QR | öğrenci → Sınav kartı | gerçek QR + jeton metni (açılır); yeni kart eskisini geçersiz kılar |
| Dışa aktarma | takvim → Excel/CSV/iCal | dosya iner, Excel Türkçe karakterleri doğru açar |
| Simülasyon | takvim → Simüle et | ceza/ihlal, yayındakiyle fark; sürüm sayısı değişmez |
| Editör | Takvim editörü → bir sınavı sürükle | uygunsa ceza farkı + "Uygula" → yeni taslak; ihlalde nedenler (×N) |
| Mobil | 390 px genişlik | yatay taşma yok, alt menü |
| Salon kapısı | sekreter → Salonlar → salon düzenle → kapı kenarı + konum | kroki kapıyı gösterir; geçersiz konum kaydedilmez |
| Salon ekle/düzenle | Salonlar → Yeni salon / düzenle | kapasite = satır×sütun−pasif koltuk; yinelenen kod hata verir |
| Mazeret kararı | öğrenci mazeret gönderir → sekreter **veya** öğrenci işleri "Onayla/Reddet" | ikinci karar 409; onaylı sınavın kartı "mazeretiniz var" |
| QR + kod yoklaması | gözetmen: Yoklama → "Yeni QR oluştur"; öğrenci: Sınav programım → kodu gir | öğrenci `geldi`; sayaç artar; "Yeni kod üret" eski kodu geçersiz kılar |
| Takvim süzgeçleri | Takvim → gün / saat / tür süzgeci | liste süzülür, sayaç güncellenir |

Canlılık ve dinamik QR duman testi (`deploy/ui-smoke/live.cjs`; API'de `QR_TTL=20s` gerekir): salon kartları çizilir, canlı veri 11 sn'de ≥3 kez yenilenir, "Son güncelleme" göstergesi var, QR jetonu kendiliğinden yenilenir ve eskisi 409 olur, tablet kamera akışı (sahte aygıt) açılır. `qr-roundtrip.cjs`: arayüzün ürettiği QR, tabletin kullandığı çözücüyle (jsQR) aynen çözülür (40–260 karakterlik jetonlar). `screens.cjs` README ekran görüntülerini üretir.

Otomatik duman testi (Playwright gerekir): `NODE_PATH=$(npm root -g) UI_URL=http://localhost:3000 node deploy/ui-smoke/pages.cjs` ve `flows.cjs`
(5 rolün tüm ekranları hatasız açılır; giriş hatası, kart, koltuk haritası, içe aktarma, tablet senkronu). Son koşu (25 Ekim 2026, Vite geliştirme sunucusu + `docker compose` API): `pages` 17/17 ekran, `flows` 8/8, `features` 15/15, `checkin` 6/6, `live` 9/9 (`QR_TTL=20s` ile), `qr-roundtrip` 5/5, `simple` 18/18 (Faz 20). `checkin`/`live` taze seed ister (`make verify` yayındaki çizelgeyi değiştirir → önce `make reset-demo`).

## Tekrarlanabilirlik ve sınırlar

- `make reset-demo`: veritabanını ilk seed haline döndürür (seed ve hash sabit). Doğrulandı.
- `make verify` yan etki bırakır (yeni çizelge sürümleri, onaylı öğrenci mazereti, çıkarılan gözetmen, bir tahliye) ve
  kasıtlı olarak tekrar çalıştırılabilir: mazeretsiz sınavlar arasından seçer. Öğrencinin mazeretsiz sınavı <2 kalırsa
  açıkça başarısız olur ve `make reset-demo` önerir.
- **Doğrulanmadı:** Windows'ta çalıştırma. GitHub Actions CI (test -race + kapsam, lint, frontend, docker) Linux'ta yeşil.
- Eşzamanlı tahliye (iki istek aynı hedefe) `make prove-t5` / DB testlerinde sınanır; `make verify` tek tahliye yapar.
