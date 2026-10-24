# syntax=docker/dockerfile:1

# ---- Derleme aşaması ----
# package.json önce kopyalanır: kaynak değiştiğinde npm ci katmanı
# önbellekten gelir (soğuk makinede 5 dk hedefi).
FROM node:22-alpine AS build

WORKDIR /app

COPY package.json package-lock.json* ./
# package-lock.json varsa ci, yoksa install. Kilit dosyası depoya eklendiğinde
# ci yolu çalışır ve derleme tekrarlanabilir olur.
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi

COPY . .

# Vite, VITE_ önekli değişkenleri derleme sırasında gömer.
ARG VITE_API_BASE_URL=http://localhost:8080
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL

RUN npm run build

# ---- Çalışma aşaması ----
FROM nginx:1.27-alpine AS runtime

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html

# nginx'i root olmayan kullanıcıyla çalıştır: PID dosyası ve geçici dizinler
# için yazma izni gereken yerleri devrediyoruz.
RUN touch /var/run/nginx.pid \
    && chown -R nginx:nginx /var/run/nginx.pid /var/cache/nginx /usr/share/nginx/html

USER nginx

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
