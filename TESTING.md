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
| F-2 | salon CRUD, koltuk düzeni, lab filtresi + keyset | kapasite = satır×sütun−pasif; yinelenen 409; geçersiz 422 | `make verify` (F-2) |
| F-3 | `POST /schedules` (202) → iş sorgula → yayınla | 2. eşzamanlı istek 409; aynı tohum → özdeş çizelge; yayın `Idempotency-Key` ile idempotent | `make verify` (F-3) |
| T-1 | API'den takvim | hiçbir öğrenci aynı slotta 2 sınav / günde >2 sınav yok; sert ihlal 0 | `make verify` (T-1) · `make prove-t1` |
| T-2 | slot–salon payları | kapasite aşılmaz, lab/klasik uyumlu, paylar oturma planıyla birebir | `make verify` (T-2) · `make prove-t2` |
| T-3 | oturma planı | yan yana aynı sınav yok, koltuk tekil, engelli öğrenci erişilebilir koltukta | `make verify` (T-3) · `make prove-t3` |
| F-4 | `/me/exams`, `/schedules/{id}/rooms`, `/seating` | öğrencinin ekranındaki salon+koltuk planla aynı | `make verify` (F-4) |
| T-4 | gözetmen görevleri | aynı slotta tek görev, ardışık oturum yok, >50 öğrenci→2 gözetmen, yük farkı ≤2 | `make verify` (T-4) · `make prove-t4` |
| F-5 | `POST /schedules/{id}/invigilators/{u}/remove` | ihlal 0, değişiklik = boşalan + takas, kalan atamalar kilitli, yanıt idempotent | `make verify` (F-5) |
| F-6 | mazeret oluştur/karara bağla, çakışma listesi | yinelenen 409, ikinci karar 409, onaylı mazeretli sınavın kartı 409 `excused` | `make verify` (F-6) |
| OWNERSHIP | başkasının verisi | öğrenci 403/404; hoca yalnız kendi dersi; gözetmen yalnız kendi salonundaki sınav; taslak 404 | `make verify` (OWNERSHIP) |
| F-7 | kart → QR → senkron | QR 200, ikinci okutma 409 `qr_already_used`; sync tekrarı `duplicates`; QR≻elle-absent çakışması `qr_present` | `make verify` (F-7) |
| T-5 | 20 eşzamanlı okutma; tahliye | tam 1×200 + 19×409; yetersiz kapasite 409 ve veri değişmez; başarılı tahliye sonrası T-3/T-4 ihlali 0 | `make verify` (T-5) · `make prove-t5` |

Birim/entegrasyon testleri (gerçek PostgreSQL ister): `make test` (yarış dedektörüyle) ya da
`TEST_DATABASE_URL=… go test ./...`. Etki alanı paketleri deterministik/saf Go'dur; DB'siz çalışanlar için `make test-short`.

## Verify'ın yakaladığı gerçek hata

İlk koşuda T-1, `GET /schedules/{id}/conflicts` uç noktasının 9.165 satır döndürdüğünü, bağımsız hesabın 9.180 çıkardığını
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
| Mobil | 390 px genişlik | yatay taşma yok, alt menü |

Otomatik duman testi (Playwright gerekir): `NODE_PATH=$(npm root -g) UI_URL=http://localhost:3000 node deploy/ui-smoke/pages.cjs` ve `flows.cjs`
(5 rolün tüm ekranları hatasız açılır; giriş hatası, kart, koltuk haritası, içe aktarma, tablet senkronu). Son koşu: 16/16 ekran + 8/8 akış geçti
(Vite geliştirme sunucusu üzerinden).

## Tekrarlanabilirlik ve sınırlar

- `make reset-demo`: veritabanını ilk seed haline döndürür (seed ve hash sabit). Doğrulandı.
- `make verify` yan etki bırakır (yeni çizelge sürümleri, onaylı öğrenci mazereti, çıkarılan gözetmen, bir tahliye) ve
  kasıtlı olarak tekrar çalıştırılabilir: mazeretsiz sınavlar arasından seçer. Öğrencinin mazeretsiz sınavı <2 kalırsa
  açıkça başarısız olur ve `make reset-demo` önerir.
- **Doğrulanmadı:** GitHub Actions CI'nın yeşil koşusu (bu ortamdan itilemedi/çalışmadı), Windows'ta çalıştırma.
- Eşzamanlı tahliye (iki istek aynı hedefe) `make prove-t5` / DB testlerinde sınanır; `make verify` tek tahliye yapar.
