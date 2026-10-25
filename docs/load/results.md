| Uç / senaryo | istek | p50 (ms) | p95 (ms) | p99 (ms) |
|---|---|---|---|---|
| GET /schedules/{id}/exams | 8683 | 49.3 | 104.9 | 140.7 |
| GET /me/exams | 8683 | 18.5 | 41.5 | 77.5 |
| GET /rooms | 8683 | 19.4 | 42.4 | 60.4 |
| GET /schedules/{id}/seating | 8683 | 34.9 | 64.8 | 87.9 |
| POST /attendance/sync (5 olay) | 15882 | 21.3 | 48.3 | 67.2 |
| **Tümü** | 50621 | 26.0 | 70.2 | 106.3 |

Hata oranı (http_req_failed): 0.000 % · toplam 50621 istek · 1256 istek/sn · 30 okuma + 10 yazma VU, 40s
