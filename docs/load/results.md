| Uç / senaryo | istek | p50 (ms) | p95 (ms) | p99 (ms) |
|---|---|---|---|---|
| GET /schedules/{id}/exams | 8343 | 37.6 | 66.0 | 83.6 |
| GET /me/exams | 8343 | 31.4 | 70.8 | 97.6 |
| GET /rooms | 8343 | 21.9 | 48.1 | 65.1 |
| GET /schedules/{id}/seating | 8343 | 39.7 | 71.4 | 93.0 |
| POST /attendance/sync (5 olay) | 14233 | 23.8 | 53.7 | 73.1 |
| **Tümü** | 47612 | 30.3 | 63.3 | 85.1 |

Hata oranı (http_req_failed): 0.000 % · toplam 47612 istek · 1181 istek/sn · 30 okuma + 10 yazma VU, 40s
