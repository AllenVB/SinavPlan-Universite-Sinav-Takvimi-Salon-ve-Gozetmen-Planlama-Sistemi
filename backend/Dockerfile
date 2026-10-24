# syntax=docker/dockerfile:1

# ---- Derleme aşaması ----
#
# Bağımlılıklar vendor/ içinde depoda tutuluyor, bu yüzden bu aşama AĞ
# GEREKTİRMEZ: go mod download adımı yok. Kazanç üç yönlü:
#   1. soğuk makinede derleme belirgin biçimde hızlanır (proje.md §3: 5 dk),
#   2. derleme tekrarlanabilir olur (modül proxy'sinin durumuna bağlı değil),
#   3. kısıtlı ağda da derlenir.
# Karşılığı depo boyutu (~17 MB); bkz. DECISIONS ADR-002.
FROM golang:1.24-alpine AS build

WORKDIR /src

# Önce modül üstverisi ve vendor ağacı: kaynak kod değiştiğinde bu katman
# önbellekten gelir.
COPY go.mod go.sum ./
COPY vendor/ ./vendor/

COPY . .

ARG VERSION=dev
# Hangi ikilinin derleneceği: api | migrate | simulator | verify
ARG TARGET=api

# CGO_ENABLED=0: tamamen statik ikili, çalışma imajında libc gerekmez.
# -trimpath: derleme yollarını ikiliden siler (hem boyut hem bilgi sızıntısı).
# -s -w: sembol ve DWARF tablolarını atar.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -mod=vendor \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/app ./cmd/${TARGET}

# ---- Çalışma aşaması ----
#
# apk add KULLANILMIYOR: ihtiyaç duyulan iki şey başka yoldan sağlanıyor.
#   - CA sertifikaları derleme aşamasından kopyalanıyor,
#   - saat dilimi verisi ikiliye gömülü (cmd/api'de `import _ "time/tzdata"`).
# Böylece çalışma imajı ağ erişimi olmadan kurulur ve küçük kalır.
FROM alpine:3.20 AS runtime

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/app /usr/local/bin/app

# Root olmayan kullanıcı, sayısal UID/GID ile. adduser çağırmıyoruz çünkü
# statik Go ikilisi /etc/passwd kaydına ihtiyaç duymuyor; bu da çalışma
# aşamasında paket kurulumunu tamamen gereksiz kılıyor.
USER 10001:10001
WORKDIR /tmp

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/app"]
