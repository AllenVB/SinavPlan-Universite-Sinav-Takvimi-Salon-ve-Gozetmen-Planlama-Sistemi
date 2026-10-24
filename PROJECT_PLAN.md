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

### P2 — Opsiyonel (çekirdek bitince)
iCal dışa aktarma · plan karşılaştırma (iki çözümün metrikleri) ·
sürükle-bırak takvim düzenleme (proje.md §11)

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
