# SınavPlan — Proje Planı

Kaynak gereksinimler: `docs/proje.md` (DDO alım projesi) + görev tanımı.
Çalışma protokolü: `CLAUDE.md`. Görev durumu: `TASKS.md`. Kararlar: `DECISIONS.md`.

## 1. Kapsam ve mimari

Üniversite sınav dönemi için: takvim çizelgeleme, salon/koltuk yerleştirme,
gözetmen atama, sınav günü canlı yoklama. Tek komutla ayağa kalkan
frontend + backend + PostgreSQL.

```
frontend (React+Vite+TS, :3000)
      │ REST/JSON
      ▼
backend (Go 1.24, chi, :8080)
  httpapi ── middleware: RequestID·recover·metrics·timeout·RBAC·Idempotency
      │
      ├── domain/  SAF GO, DB bağımlılığı yok, kapsam ≥%70
      │     scheduling(T-1) rooms(T-2) seating(T-3) invigilation(T-4) attendance(T-5)
      │
      └── store/   pgxpool + sqlc + repository'ler
                      │
                      ▼
            PostgreSQL 16 (compose ağında, host'a kapalı)
                      ▲
            migrate(goose, tek seferlik) · seed/simulator(--seed=42)
```

**Neden domain katmanı saf:** çekirdek görevler puanın %27'si (40/150) ve test
kapsamı ≥%70 şartı burada. DB'den ayrıldığında hızlı, deterministik ve
testcontainers gerektirmeyen testler yazılabiliyor.

## 2. Öncelik sınıflandırması

### P0 — Kritik (bunlar olmadan teslim sayılmaz)

| # | Gereksinim | Ölçülebilir kabul kriteri |
|---|---|---|
| P0-1 | Tek komutla ayağa kalkma | `cp .env.example .env && docker compose up --build` soğuk makinede ≤5 dk; api/frontend/db healthy |
| P0-2 | `GET /health` | DB varken 200, postgres durdurulunca 503 + düşen bağımlılık adı |
| P0-3 | Şema + migrasyon + seed otomatik | compose sonrası elle adım yok; 300 sınav/8000 öğrenci/60 salon yüklü |
| P0-4 | Deterministik seed | aynı `--seed` → aynı veri; hash karşılaştırmalı test geçer |
| P0-5 | Auth + RBAC | argon2id; JWT 15dk/7g; kimliksiz 401, yetkisiz 403, başkasının verisi 403/404 — testle |
| P0-6 | T-1 çizelgeleme | 300/8000/15gün ≤60sn, **0 sert ihlal**, metrik basar; `make prove-t1` |
| P0-7 | T-2 salon atama | kapasite/tip ihlali 0, deterministik (aynı girdi=aynı çıktı testi), doğrulayıcı |
| P0-8 | T-3 oturma | `CountViolations()==0`; her öğrenciye tam 1 koltuk; bozuk planda doğru sayı |
| P0-9 | T-4 gözetmen | tüm kısıtlar sağlanır; max−min raporlanır; `Reassign` yalnız gerekli atamayı değiştirir |
| P0-10 | T-5 yoklama | QR tek kullanımlık: 50 goroutine → **tam 1 başarı + 49×409** (gerçek Postgres) |
| P0-11 | T-5 senkron | tekrar olaylar yok sayılır; çakışma kuralı permutasyondan bağımsız aynı sonuç |
| P0-12 | F-1..F-7 uçları | her biri `requests.http`'den demo hesapla çalışır |
| P0-13 | `make verify` | F-1..F-7 + T-1..T-5 + health + 401/403 tablo basar, hata varsa exit 1, <15 dk |
| P0-14 | Test + CI | ≥40 test, domain ≥%70, `go test -race`, CI'da lint+test+docker build |
| P0-15 | Frontend çalışır | gerçek veri, yükleniyor/boş/hata durumları, mobil genişlik (proje.md §5) |

### P1 — Önemli (değerlendirmeyi maddi olarak etkiler)
Idempotency-Key · append-only audit (trigger) · keyset sayfalama · `/metrics` ·
OpenAPI + `/docs` · `make load` (k6 p50/p95/p99) · `make chaos` ·
EXPLAIN betiği + `docs/explain/` · `DECISIONS.md` ≥5 ADR · README Doğrulama Soruları ·
`TESTING.md` · `make reset-demo` · simülatör `--live` modu

### P2 — Fark yaratan özellikler (Faz 11, kullanıcı kararıyla plana alındı)

| # | Özellik | Kabul ölçütü |
|---|---|---|
| X-1 | QR görseli | sınav kartında tarayıcının okuyabileceği gerçek QR; jeton değişmez |
| X-2 | Dışa aktarma | takvim+gözetmen `.xlsx` ve `.csv` (sunucuda üretilir, Excel açar); PDF = yazdırma stili (tarayıcıdan PDF) |
| X-3 | iCal | `.ics`: öğrenci programı, gözetmen görevleri, tüm takvim; UTC, kararlı UID |
| X-4 | Hızlı simülasyon | kaydetmeden T-1 çalıştır: ceza/ihlal/süre, yayındaki sürümle fark |
| X-5 | Plan karşılaştırma | iki sürümün metrik farkı + taşınan sınavlar listesi |
| X-6 | Etkinlik akışı | panelde gerçek denetim kaydından son olaylar (sahte veri yok) |
| X-7 | Sürükle-bırak editör | sınavı slota sürükle; sert kısıt ihlalinde reddet (neden listesiyle); başarılıysa T-2..T-4 yeniden kurulur ve **yeni taslak sürüm** oluşur |
| X-8 | ÖBS | **kapsam dışı (canlı entegrasyon):** harici sistem erişimi yok; sahte bağlantı yapılmaz. Yerine mevcut CSV içe aktarma (F-1) ÖBS dışa aktarımını karşılar |

Kural: her özellik API'de gerçek veriye dayanır, testi ve `make verify` kimliği vardır; test edilmeyen özellik "yapıldı" sayılmaz.

## 2b. Faz 12–15: Canlı arayüz (kullanıcı kararı: tasarımlar kadar "canlı" olsun)

Kullanıcı geri bildirimi: arayüz doğru ama tasarım zip'lerindeki gibi canlı değil (kırmızı/yeşil durum ışıkları, renkli
tablolar, grafikler) ve QR kodlar sürekli yenilenen/çalışan olmalı. Kural aynı: **veri sahte olamaz**; her canlı öğe gerçek
API verisine bağlıdır, "Son güncelleme: N sn önce" göstergesi gerçek yenileme zamanını yansıtır.

**Arka uç denetimi (bu planın girdisi):** 165 test + `make verify` 21/21 geçiyor. Canlı arayüz için eksikler:
(1) salon bazlı canlı yoklama özeti yok (yalnız sınav bazlı), (2) etkinlik akışı filtre/artımlı okuma/sayfalama sunmuyor,
(3) QR jetonu sınav bitimine +1 saat geçerli — "dinamik QR" için kısa ömür ve yenileme yok, (4) sunucudan itme (SSE/WebSocket)
yok → kısa aralıklı yoklama (polling) ile canlılık, artımlı uçlarla maliyet düşük tutulur, (5) tablet için kamera tarama yok.

| Faz | Kapsam | Kabul ölçütü |
|---|---|---|
| **12 Tasarım sistemi v2** ✅ | durum ışıkları (yeşil/sarı/kırmızı nokta + renkli çipler), ikonlu istatistik kartları, ilerleme çubukları, SVG grafikler (yığılmış sütun, histogram), avatar, üst çubukta "Canlı • son güncelleme", bildirim zili (gerçek sayaç), renkli tablo satırları; Ana panel, Takvim, Salon, Gözetmen, Mazeret, Çakışma, İçe aktarma ekranlarına uygulama | tüm ekranlar duman testinde hatasız; ekran görüntüleri tasarımla yan yana kıyaslanır; kontrast ≥ 4.5:1 |
| **13 Canlı operasyon (arka uç + ekran)** ✅ | `GET /schedules/{id}/live` (salon bazlı gelen/gelmedi/işaretlenmemiş, son olay, gözetmenler); `/activity` filtre (`action`, `actor`), `after_id`/`before_id` artımlı okuma; denetim kaydı tablosu; `useLive` (görünür sekmede yenile, gizliyken dur, hata→geri çekilme); Sınav günü izleme ekranı: salon kartları + canlı olay akışı + denetim tablosu | HTTP testleri (toplamlar seating×attendance ile birebir, yetki, filtre); `make verify` L-1; canlı simülatör çalışırken sayılar değişir |
| **14 Dinamik QR ve tablet** ✅ | QR jetonu kısa ömürlü (`QR_TTL`, varsayılan 2 dk) ve istemci 45 sn'de bir yeniler (geri sayım halkası; her yenileme eskiyi iptal eder); öğrenci ekranı: "sıradaki sınav" + geri sayım + dönen QR; tablet: kamera ile QR okuma (jsQR), çevrimdışı şeridi, istatistik kartları, satır renkleri, tahliye protokolü adımları | QR döndürme birim/HTTP testi (eski jeton 409/410, yenisi 200); jsQR ile üretilen QR'ın çözülmesi testi; UI duman |
| **15 Cila ve teslim** ✅ (kısmen: öğretim üyesi ekranı ayrı tasarlanmadı; takvim+oturma ekranlarını kullanır) | Gözetmen yük histogramı + kurallar kartı, Öğretim üyesi ekranı, boş/yükleniyor/hata durumları her yerde, mobil ve klavye erişimi, ekran görüntüleri `docs/screens/`, README/TESTING/ADR güncellemesi | duman testi + `make verify` hepsi geçer; bilinen sınırlar dürüstçe yazılı |

Bu fazların hiçbiri P0'ı etkilemez; gecikme olursa sırayla düşer: 15 → 14'ün kamera kısmı → 13'ün denetim tablosu.

### Faz 16 — Fakülte yapısı (kullanıcı isteği) ✅

4 fakülte / 15 bölüm / 1–4. sınıf × 5 ders, adlı Mühendislik hocaları, fakülte → bölüm → sınıf süzgeci ve kapsamlı sınav provası. Kabul: `make verify` FAK-1, `simdata` yapı testleri, HTTP kapsam testleri, UI duman (F16). Ayrıntı ve varsayımlar: ADR-016.

## 3. Kritik yol

```
Faz1 iskelet ─► Faz2 şema+seed ─► DİKEY DİLİM (giriş→öğrenci programı, uçtan uca)
                                        │
            ┌───────────────────────────┴────────────┐
            ▼                                        ▼
   Faz3 auth/RBAC/idempotency/audit        Faz4-7 T-1..T-5 (saf domain, paralel yazılabilir)
            └───────────────┬────────────────────────┘
                            ▼
                   Faz8 F-1..F-6 uçları ─► Faz8.5 frontend ─► Faz9 verify ─► Faz10 ölçüm+doküman
```

Domain paketleri DB'ye bağlı olmadığı için Faz 3 ile Faz 4-7 birbirini
bloke etmiyor — gecikme olursa domain işi önce gider (puan ağırlığı yüksek).

## 4. Zaman bütçesi (24 saat)

| Dilim | Hedef | Durum |
|---|---|---|
| İnceleme + planlama | ~1,5 sa (%6) | tamamlandı |
| Çekirdek uygulama | ~12 sa (%50) | Faz 1 sürüyor |
| Entegrasyon + test | ~5,5 sa (%23) | — |
| Düzeltme + teslim hazırlığı | ~5 sa (%21) | — |

Gecikme halinde sırayla düşer: P2 → `make load`/`chaos` cilası → frontend ekran
sayısı (6'dan 4'e) → bonus dokümantasyon. **Düşmeyecekler:** P0-1..P0-14.

## 5. `docs/proje.md` ile görev tanımı farkı

Görev tanımı `proje.md`'nin daraltılmış üst kümesi; teknoloji seçimlerini
sabitliyor (chi, pgx/v5+sqlc, goose, argon2id, JWT, React+Vite+TS) —
`proje.md` bunları serbest bırakıyordu, daraltma geçerli.

**Tek gerçek çelişki:** `proje.md` §5 frontend'i zorunlu kılıyor (gerçek veri,
yükleniyor/boş/hata, mobil); görev tanımı "placeholder bırak" diyordu.
§15'teki 50 puanlık yönetici incelemesi çalışan arayüze dayandığı için
frontend P0 kabul edildi (Faz 8.5).

`proje.md`'de olup görev tanımında olmayanlar: README'de frontend seçim
gerekçesi (§2) · `make verify`'ın arayüzü de denemesi (§10.1) · teslim hijyeni
(§13: PUBLIC depo, tek commit kabul edilmez, force-push yasak) · §12'deki 5
doğrulama sorusunun birebir metni · iki ek bonus (sürükle-bırak, plan karşılaştırma).

## 6. Varsayımlar (soru bekletmeden ilerlendi, CLAUDE.md §2)

| # | Varsayım | Gerekçe |
|---|---|---|
| V-1 | Komşuluk = sol/sağ; ön/arka "sıkı mod" opsiyonel, varsayılan kapalı | `proje.md` §6 "**yan yana** oturmamalı" |
| V-2 | Tek sınavlı salon etkin kapasite `rows×ceil(cols/2)`; karışık salonda en büyük grup ≤ etkin koltukların yarısı | T-2/T-3 etkileşimi, DECISIONS ADR-003 |
| V-3 | Takvim durumu yalnız `draft`/`published` + versiyon; `archived` yok | "farklı aşamalar yapma" talimatı |
| V-4 | `lecturer` ve `invigilator` ayrı rol, bir kullanıcı ikisini taşıyabilir | §6 "kendi dersinde gözetmen olmamalı" kısıtı aksi halde anlamsız |
| V-5 | Eşzamanlı aynı Idempotency-Key → 409 (beklemek yerine) | deterministik ve test edilebilir |
| V-6 | Mazeret onayı telafi slotu üretmez; kayıt + muafiyet | `proje.md` telafi çizelgeleme istemiyor |
| V-7 | Depo adı mevcut hâliyle kalır (`ddo-case-...` yalnız öneri) | §13.1 "öneri" diyor |
| V-8 | İki DB rolü: `migrator` (sahip) + `app` (kısıtlı) | append-only audit için app'ten UPDATE/DELETE geri alınmalı |

## 7. Model kullanımı (fiyat/performans, CLAUDE.md §3)

Model, kullanıcı tarafından `/model` ile değiştirilir; ajan kendi modelini
değiştiremez. Her faz başında aşağıdaki tabloya bakılır.

| Faz | İş türü | Model | Effort |
|---|---|---|---|
| 1 iskelet (kalan), 2 şema+seed | rutin: Docker, SQL DDL, üretici | **Sonnet 5.5** | medium |
| 3 auth/RBAC/idempotency | güvenlik + eşzamanlılık | **Opus 5.5** | high |
| 4 T-1 çizelgeleme | algoritma + performans (60 sn) | **Opus 5.5** | high |
| 5 T-2 + T-3 | algoritma, orta zorluk | Sonnet 5.5 → takılırsa Opus | medium |
| 6 T-4 + Reassign | minimum-değişiklik garantisi | **Opus 5.5** | high |
| 7 T-5 + yarış testleri | eşzamanlılık, FOR UPDATE, 50 goroutine | **Opus 5.5** | high |
| 8 F-1..F-6, OpenAPI, requests.http | rutin CRUD | **Sonnet 5.5** | medium |
| 8.5 frontend | rutin UI | **Sonnet 5.5** | medium |
| 9 verify, TESTING.md | rutin senaryo yazımı | **Sonnet 5.5** | medium |
| 10 k6, chaos, EXPLAIN, README, DECISIONS | rutin betik + doküman | **Sonnet 5.5** | low–medium |
| Zor hata ayıklama (her fazda) | kök neden analizi | **Opus 5.5** | high |

Kural: Opus yalnızca algoritma, eşzamanlılık, güvenlik ve zor hata ayıklama
içindir. Haiku bu projede kod üretimi için önerilmez (kalite riski düzeltme
maliyetini aşar). 7 günlük kullanım limiti uyarı durumunda olduğundan
rutin fazlarda Sonnet'e geçmek limitin dolmasını geciktirir.
