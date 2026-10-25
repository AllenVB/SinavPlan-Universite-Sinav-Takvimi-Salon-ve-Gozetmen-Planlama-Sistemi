# İnternette yayın: Vercel (arayüz) + Render (API) + Neon (PostgreSQL)

Bu yayın **isteğe bağlıdır**; değerlendirme için `docker compose up --build` yeterlidir. Üç hesap da ücretsiz
katmanda çalışır; yayını hesap sahibi yapar (bu depodan otomatik yayın yok, `autoDeploy: false`).

```
tarayıcı ──> Vercel (statik React)  ──/api/*──>  Render (Go API, Docker)  ──>  Neon (PostgreSQL 16)
```
Arayüz her zaman kendi kaynağına `/api/...` der; Vercel bu yolu API'ye vekiller (`frontend/vercel.json`), bu
yüzden CORS gerekmez — yerelde nginx'in yaptığı işin aynısı.

## 1. Neon — veritabanı
1. neon.tech'te proje aç (bölge: Frankfurt, PostgreSQL 16). **Connection string**'i kopyala (sahip rol,
   `?sslmode=require` ile). Bu `OWNER_URL`'dir; yalnız kurulumda kullanılır.
2. Yerelde (Docker gerekli), depo kökünde:
   ```bash
   OWNER_URL='postgres://neondb_owner:…@ep-….eu-central-1.aws.neon.tech/neondb?sslmode=require' \
   APP_DB_PASSWORD='en-az-12-karakterlik-güçlü-parola' \
   ./deploy/remote/migrate-seed.sh
   ```
   Betik migrasyonları uygular, kısıtlı uygulama rolünü (`sinavplan_app`; denetim kaydında UPDATE/DELETE yok)
   kurar, 301 sınavlık demo verisini yükler ve en sonda API için **DATABASE_URL**'i yazar.
   Windows'ta: Git Bash veya WSL ile çalıştırın.

## 2. Render — API
1. Render → **New → Blueprint** → bu depo. `render.yaml` okunur (`sinavplan-api`, Docker, `/health`).
2. İstenince `DATABASE_URL` olarak 1. adımın çıktısını girin. `JWT_SECRET` ve `QR_HMAC_SECRET` Render
   tarafından rastgele üretilir (depoda sır yok).
3. Yayın bitince `https://sinavplan-api.onrender.com/health` → `{"status":"healthy",…}`. Servis adı farklı
   olursa adresi not edin.

## 3. Vercel — arayüz
1. Vercel → **Add New → Project** → bu depo, **Root Directory: `frontend`** (Vite otomatik algılanır).
2. Render adresi `sinavplan-api.onrender.com` değilse `frontend/vercel.json` içindeki `destination`'ı düzeltin.
3. Yayın adresini açın, demo hesaplarıyla girin (`sekreter@sinavplan.test` / `Demo!1234`).

## Bilinmesi gerekenler
- **Demo hesapları herkese açık parolayla gelir** ve veriler kurgusaldır; gerçek kişisel veri girmeyin.
  Yayını kapatmak için Render servisini askıya almak yeterli.
- **"Şu an süren sınav" seed anına göredir**: Prompt Mühendisliği oturumu seed'den 20 dk önce başlamış sayılır ve
  ~90 dk sürer. Canlı izleme/yoklama göstermeden hemen önce betiği yeniden çalıştırmak yetmez (veri zaten
  yüklüyse seed atlar); Neon'da veritabanını sıfırlayıp (Branch → Reset) betiği yeniden çalıştırın.
- Render ücretsiz katmanında servis 15 dk boşta kalınca uyur; ilk istek ~30–60 sn sürebilir.
- Yoklama kodu deneme sayacı veritabanında olduğundan API birden çok örnekle de doğru çalışır (ADR-019).

## Doğrulama (bu ortamda yapılan)
Gerçek Vercel/Render/Neon hesabıyla **denenmedi** (hesaplar yazara ait değil). Yerelde Neon taklidi yapıldı:
compose Postgres'te ayrı bir veritabanı + ayrı uygulama rolü ile `migrate-seed.sh` çalıştırıldı, API
`APP_ENV=production` ile bu DATABASE_URL'e bağlandı: `/health` healthy, `ogrenci@` girişi 200, kısıtlı rol
`UPDATE audit_log` → `permission denied`. `vercel.json` ve `render.yaml` yalnız sözdizimi olarak denetlendi.
