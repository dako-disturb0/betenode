# 🟢 BetterLyrics Node on Google Apps Script (GAS)

Node accelerator serverless 100% gratis menggunakan Google Apps Script untuk mempercepat BetterLyrics.

---

## 🚀 Cara Deploy (3 Langkah Mudah)

1. Buka [script.google.com](https://script.google.com/) dan klik **New project**.
2. Salin isi file [`Code.js`](file:///home/container/betterlyrics-node/bete-node/GAS/Code.js) ke dalam editor script Google.
3. Klik tombol **Deploy** (kanan atas) ➔ **New deployment**:
   - Pilih tipe deployment: **Web app** (ikon roda gigi ⚙️)
   - **Execute as**: `Me (<email-anda>)`
   - **Who has access**: `Anyone` *(Penting agar bisa diakses oleh Origin)*
   - Klik **Deploy**.
4. Salin **Web app URL** yang muncul (misalnya: `https://script.google.com/macros/s/AKfycb.../exec`).

---

## 🔗 Menghubungkan ke Origin

Masukkan URL Web App GAS ke file `.env` Origin Anda:

```env
# Tambahkan node GAS:
NODE_GAS=https://script.google.com/macros/s/AKfycb.../exec
# atau sebagai NODE1 / NODE2:
NODE1=https://script.google.com/macros/s/AKfycb.../exec
```

---

## 📡 Fitur GAS Node
- **`GET /exec`**: Mengembalikan status telemetry `/interconnect` dan statistik cache Google.
- **`POST /exec`**: Proxy streaming lirik `POST /v2/lyrics` dengan caching otomatis di Google CacheService.
- **`GET /exec?path=health`**: Healthcheck ringan (Return `OK`).
