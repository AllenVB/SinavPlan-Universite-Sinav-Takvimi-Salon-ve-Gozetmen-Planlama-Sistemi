# SınavPlan — geliştirme ve doğrulama komutları
#
# Hızlı başlangıç:
#   cp .env.example .env && docker compose up --build
#
# Değerlendirme:
#   make verify     uçtan uca doğrulama tablosu (<15 dk)
#   make prove-t1   T-1 çizelgeleme kanıtı
#   make test       birim testleri + kapsam

SHELL := /bin/bash

# .env varsa değişkenlerini Makefile'a da al (POSTGRES_* vb.).
-include .env
.DEFAULT_GOAL := help

COMPOSE     := docker compose
BACKEND_DIR := backend
COVER_FILE  := coverage.out
# Kapsam şartı (≥%70) yalnızca saf domain paketleri için geçerli.
DOMAIN_PKGS := ./internal/domain/...

.PHONY: help
help: ## Komut listesini göster
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
	  | sort \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- compose

.PHONY: up
up: .env ## Tüm yığını derle ve başlat
	$(COMPOSE) up --build

.PHONY: up-d
up-d: .env ## Yığını arka planda başlat
	$(COMPOSE) up --build -d

.PHONY: down
down: ## Servisleri durdur (veri korunur)
	$(COMPOSE) down

.PHONY: clean
clean: ## Servisleri durdur ve veritabanı birimini SİL
	$(COMPOSE) down -v

.PHONY: logs
logs: ## api log'larını izle
	$(COMPOSE) logs -f api

.PHONY: ps
ps: ## Servis durumlarını göster
	$(COMPOSE) ps

.env:
	@echo ".env bulunamadı, .env.example'dan kopyalanıyor"
	@cp .env.example .env

# ---------------------------------------------------------------- test

.PHONY: test
test: ## Tüm testleri yarış dedektörüyle çalıştır
	cd $(BACKEND_DIR) && go test -race ./...

.PHONY: test-short
test-short: ## Yalnızca hızlı testler (Postgres gerektirenler atlanır)
	cd $(BACKEND_DIR) && go test -short -race ./...

.PHONY: test-integration
test-integration: .env ## DB testleri: compose ağındaki postgres'e karşı, konteyner içinde (-race)
	$(COMPOSE) up -d postgres
	docker run --rm --network sinavplan_default -v "$(CURDIR)/backend:/src" -w /src \
	  -e TEST_DATABASE_URL="postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres:5432/postgres?sslmode=disable&connect_timeout=5" \
	  golang:1.24 go test -mod=vendor -race -count=1 ./...

.PHONY: cover
cover: ## Kapsam raporu üret; domain paketleri için özet bas
	cd $(BACKEND_DIR) && go test -race -coverprofile=$(COVER_FILE) -covermode=atomic ./...
	@echo ""
	@echo "=== Paket bazında kapsam ==="
	cd $(BACKEND_DIR) && go tool cover -func=$(COVER_FILE) | tail -1
	@echo ""
	@echo "=== Domain paketleri (şart: >= %70) ==="
	cd $(BACKEND_DIR) && go test -cover $(DOMAIN_PKGS) 2>/dev/null \
	  | grep -E 'coverage|no test files' || true

.PHONY: cover-html
cover-html: cover ## Kapsam raporunu HTML olarak üret
	cd $(BACKEND_DIR) && go tool cover -html=$(COVER_FILE) -o coverage.html
	@echo "backend/coverage.html hazır"

.PHONY: lint
lint: ## golangci-lint çalıştır
	cd $(BACKEND_DIR) && golangci-lint run ./...

.PHONY: fmt
fmt: ## gofmt uygula
	cd $(BACKEND_DIR) && gofmt -w $$(find . -name '*.go' -not -path './internal/store/sqlc/*')

.PHONY: tidy
tidy: ## go mod tidy
	cd $(BACKEND_DIR) && go mod tidy

# ---------------------------------------------------------------- kanıt


.PHONY: demo
demo: ## Canlı izleme gösterisi: çizelge yoksa üretir/yayınlar, sonra ilk oturum için yoklama akıtır (90 sn)
	$(COMPOSE) --profile demo run --rm demo

.PHONY: verify
verify: ## Uçtan uca doğrulama: F-1..F-7, T-1..T-5, health, 401/403 (<15 dk)
	$(COMPOSE) --profile verify run --rm verify

.PHONY: prove-t1
prove-t1: ## T-1 kanıtı: 300 sınav/8.000 öğrenci/15 gün -> süre, ihlal, metrikler
	cd $(BACKEND_DIR) && go run ./cmd/prove t1

.PHONY: prove-t2
prove-t2: ## T-2 kanıtı: salon atama (ihlal, salon sayısı, determinizm)
	cd $(BACKEND_DIR) && go run ./cmd/prove t2

.PHONY: prove-t3
prove-t3: ## T-3 kanıtı: oturma düzeni (ihlal sayacı, bozuk plan sınaması)
	cd $(BACKEND_DIR) && go run ./cmd/prove t3

.PHONY: prove-t4
prove-t4: ## T-4 kanıtı: gözetmen atama, adalet, minimum değişiklikli yeniden atama
	cd $(BACKEND_DIR) && go run ./cmd/prove t4

.PHONY: prove-t5
prove-t5: .env ## T-5 kanıtı (gerçek Postgres): 50 eşzamanlı QR, eşzamanlı tahliye, senkron, permütasyon
	$(COMPOSE) up -d postgres
	docker run --rm --network sinavplan_default -v "$(CURDIR)/backend:/src" -w /src \
	  -e TEST_DATABASE_URL="postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres:5432/postgres?sslmode=disable&connect_timeout=5" \
	  golang:1.24 go test -mod=vendor -race -count=1 -v \
	  -run 'Resolution|TotalOrder|HLC|FiftyConcurrent|Evacuation|ConcurrentSync|Idempotent|Conflict|QRRetry|ExamCardFlow' \
	  ./internal/domain/attendance/ ./internal/store/ ./internal/httpapi/

.PHONY: prove-all
prove-all: ## T-1..T-5 kanıt komutlarını sırayla çalıştır
	cd $(BACKEND_DIR) && go run ./cmd/prove all

.PHONY: bench
bench: ## Domain benchmark'larını çalıştır
	cd $(BACKEND_DIR) && go test -run=XXX -bench=. -benchmem $(DOMAIN_PKGS)

# ---------------------------------------------------------------- veri

.PHONY: reset-demo
reset-demo: ## Veritabanını ilk seed haline döndür
	$(COMPOSE) run --rm migrate reset
	$(COMPOSE) run --rm seed seed --seed=$${SEED:-42}
	@echo "demo verisi ilk haline döndürüldü"

.PHONY: seed
seed: ## Seed'i yeniden çalıştır (mevcut veriye upsert)
	$(COMPOSE) run --rm seed seed --seed=$${SEED:-42}

.PHONY: psql
psql: ## Veritabanına psql ile bağlan
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER:-sinavplan} -d $${POSTGRES_DB:-sinavplan}

.PHONY: explain
explain: ## Dizinsiz/dizinli EXPLAIN (ANALYZE) çıktılarını docs/explain/ altına yaz
	./deploy/explain/run_explain.sh

# ---------------------------------------------------------------- ölçüm

.PHONY: load
load: ## k6 yük testleri -> p50/p95/p99 ve hata oranı
	./deploy/k6/run_load.sh

.PHONY: chaos
chaos: ## Kaos: postgres'i durdur, /health 503 doğrula, geri getir, toparlanmayı doğrula
	./deploy/chaos/chaos.sh

# ---------------------------------------------------------------- araçlar

.PHONY: tools
tools: ## Geliştirme araçlarını kur (sqlc, goose)
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0
	go install github.com/pressly/goose/v3/cmd/goose@v3.22.1

.PHONY: sqlc
sqlc: ## queries/ içinden Go kodu üret
	cd $(BACKEND_DIR) && sqlc generate

.PHONY: build
build: ## Go ikililerini derle
	cd $(BACKEND_DIR) && go build -o bin/ ./cmd/...
