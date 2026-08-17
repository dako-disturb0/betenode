# 🎼 PROJECT BREAKDOWN: BetterLyrics Multi-Node Accelerator (`bete-node`)

Dokumen ini berisi arsitektur lengkap, mekanisme kerja, spesifikasi integrasi, serta panduan deployment untuk sistem **Origin** dan **Edge Node** pada **BetterLyrics**.

---

## 📑 Daftar Isi
1. [Arsitektur Sistem](#1-arsitektur-sistem)
2. [Struktur Direktori Proyek](#2-struktur-direktori-proyek)
3. [Mode Operasi (Origin vs Node)](#3-mode-operasi-origin-vs-node)
4. [Sistem Konfigurasi & Auto-Discovery .env](#4-sistem-konfigurasi--auto-discovery-env)
5. [Protokol Interconnect & Healthcheck Node](#5-protokol-interconnect--healthcheck-node)
6. [Mekanisme Caching & Akselerasi SSE Stream](#6-mekanisme-caching--akselerasi-sse-stream)
7. [Panduan Deployment Multi-Platform](#7-panduan-deployment-multi-platform)
   - [A. Koyeb](#a-koyeb)
   - [B. Vercel](#b-vercel)
   - [C. Netlify](#c-netlify)
   - [D. Pterodactyl Panel](#d-pterodactyl-panel)
   - [E. VPS Linux (Systemd & Binary)](#e-vps-linux-systemd--binary)
   - [F. Docker & Docker Compose](#f-docker--docker-compose)
8. [Tabel Environment Variables](#8-tabel-environment-variables)
9. [Verifikasi & Endpoint API](#9-verifikasi--endpoint-api)

---

## 1. Arsitektur Sistem

BetterLyrics menggunakan SSE (*Server-Sent Events*) `POST /v2/lyrics` untuk mengambil lirik tersinkronisasi (LRC, QRC, TTML, RichSync, LineSync, Plain).

```
                        ┌──────────────────────────────────────────────┐
                        │        BetterLyrics Web / Extension          │
                        └──────────────────────┬───────────────────────┘
                                               │
                                       (POST /v2/lyrics)
                                               ▼
     ┌─────────────────────────────────────────────────────────────────────────────────┐
     │                      ORIGIN ORCHESTRATOR (Load Balancer)                        │
     │  - Priority In-Memory LRU Cache (< 1ms Instant Hit)                             │
     │  - Dynamic Node Pool (Healthcheck & Latency Scorer)                             │
     │  - Fallback Circuit Breaker                                                     │
     └─────────────┬───────────────────────────┬───────────────────────────┬───────────┘
                   │ (Fastest Latency)         │ (Backup Route)            │ (Failover)
                   ▼                           ▼                           ▼
        ┌──────────────────────┐    ┌──────────────────────┐    ┌──────────────────────┐
        │      EDGE NODE 1     │    │      EDGE NODE 2     │    │   UPSTREAM OFFICIAL  │
        │   (Koyeb / Vercel)   │    │  (VPS / Pterodactyl) │    │ (lyrics.api.dacube..)│
        │                      │    │                      │    │                      │
        │ - Local Memory Cache │    │ - Local Memory Cache │    │                      │
        │ - Upstream Pooler    │    │ - Upstream Pooler    │    │                      │
        │ - /interconnect API  │    │ - /interconnect API  │    │                      │
        └──────────┬───────────┘    └──────────┬───────────┘    └──────────────────────┘
                   │                           │
                   └─────────────┬─────────────┘
                                 ▼
                   ┌───────────────────────────┐
                   │ Upstream Providers / APIs │
                   │   (Musixmatch, LRCLib,    │
                   │    BiniLyrics, Kugou)     │
                   └───────────────────────────┘
```

---

## 2. Struktur Direktori Proyek

```
bete-node/
├── cmd/
│   ├── app/
│   │   └── main.go                 # Universal entrypoint (Auto/Manual mode)
│   ├── origin/
│   │   └── main.go                 # Standalone binary khusus Origin
│   └── node/
│       └── main.go                 # Standalone binary khusus Edge Node
├── api/
│   └── index.go                    # Serverless adapter (Vercel & Netlify)
├── internal/
│   ├── config/
│   │   └── config.go               # Multi-path .env loader & NODE[N] parser
│   ├── platform/
│   │   └── detector.go             # Deteksi otomatis Pterodactyl, Koyeb, Vercel, dll.
│   ├── cache/
│   │   └── memory_cache.go         # Thread-safe LRU Cache & SHA-256 Key Hash
│   ├── upstream/
│   │   └── client.go               # High-concurrency HTTP/2 client
│   ├── node/
│   │   └── handler.go              # Edge Node proxy, /interconnect, SSE Streamer
│   └── origin/
│       ├── pool.go                 # Dynamic Node Pool & Health Scorer
│       └── handler.go              # Smart balancer & failover router
├── deployments/
│   ├── koyeb/
│   │   └── koyeb.yaml              # Konfigurasi deploy Koyeb
│   ├── vercel/
│   │   └── vercel.json             # Konfigurasi deploy Vercel
│   ├── netlify/
│   │   └── netlify.toml            # Konfigurasi deploy Netlify
│   ├── pterodactyl/
│   │   ├── egg-bete-node.json      # Egg template Pterodactyl Panel
│   │   └── start.sh                # Startup wrapper /home/container
│   └── vps/
│       ├── install.sh              # 1-line auto installer
│       ├── bete-node.service       # Systemd service unit
│       └── docker-compose.yml      # Docker Compose setup
├── .env.example                    # Template konfigurasi lengkap
├── Dockerfile                      # Multi-stage lightweight image
├── Makefile                        # Cross-compiler (Linux, Windows, Arm64)
├── README.md                       # Ringkasan cepat
└── PROJECT.md                      # Dokumentasi komprehensif ini
```

---

## 3. Mode Operasi (Origin vs Node)

| Mode | Cara Mengaktifkan | Fungsi Utama |
|---|---|---|
| **AUTO** (Default) | `MODE=auto` | Jika mendeteksi variabel `NODE1`, `NODE2`, dll. atau `ROLE=origin`, otomatis berjalan sebagai **Origin**. Jika tidak ada daftar node, otomatis berjalan sebagai **Edge Node**. |
| **ORIGIN** | `MODE=origin` atau flag `-mode=origin` | Bertindak sebagai load balancer dan agregator node edge. Memeriksa latensi node, mendistribusikan request lirik, dan fallback ke upstream official jika seluruh node mati. |
| **NODE** | `MODE=node` atau flag `-mode=node` | Bertindak sebagai edge worker yang dideploy dekat dengan pengguna. Menyediakan endpoint `/interconnect` dan cache proxy super cepat. |

---

## 4. Sistem Konfigurasi & Auto-Discovery `.env`

Sistem secara otomatis mencari dan me-load file `.env` dengan urutan prioritas:
1. **Flag CLI / Env Eksplisit**: `-env /custom/path/.env` atau `ENV_FILE=/custom/path/.env`
2. **Direktori Binary Executable**: `./.env` dan `./bete-node/.env`
3. **Pterodactyl Container**: `/home/container/bete-node/.env` dan `/home/container/.env`
4. **Current Working Directory ($PWD)**: `$PWD/bete-node/.env` dan `$PWD/.env`
5. **OS Environment Variables / Export**: Membaca variabel langsung dari sistem (`export NODE1=...`).

### Format Penambahan Dynamic Node:
Origin memindai key dengan pola regex `^NODE_?([0-9]+)$`:
```env
NODE1=https://node-sg.koyeb.app/interconnect
NODE2=https://node-us.vercel.app/interconnect
NODE3=node-eu.vps.com/interconnect
NODE4=192.168.1.100:8080/interconnect
```
*Catatan: Protokol `https://` atau `http://` serta endpoint path akan dinormalisasi secara otomatis jika tidak disertakan.*

---

## 5. Protokol Interconnect & Healthcheck Node

Setiap **Edge Node** mengekspos endpoint `GET /interconnect` yang mengembalikan telemetry JSON:
```json
{
  "status": "online",
  "role": "edge_node",
  "version": "1.0.0",
  "platform": "KOYEB",
  "uptime_sec": 14205,
  "goroutines": 12,
  "memory_mb": 8.42,
  "cache": {
    "total_items": 1250,
    "max_items": 50000,
    "hits": 3410,
    "misses": 420,
    "hit_rate": "89.03%"
  },
  "timestamp": 1723912800,
  "server_time": "2026-08-17T16:30:00Z"
}
```

### Mekanisme Healthcheck di Origin:
- Background goroutine melakukan ping ke semua node setiap `HEALTHCHECK_INTERVAL_SEC` (default: 15 detik).
- Mengukur latency round-trip (*ms*).
- Jika sebuah node gagal merespons sebanyak 2x berturut-turut, node ditandai sebagai `unhealthy` dan dilewati dari routing hingga kembali pulih.
- Request lirik akan diarahkan ke node aktif dengan **latency terendah (Best Node First)**.

---

## 6. Mekanisme Caching & Akselerasi SSE Stream

1. **Deterministic Cache Key**:
   - Dibuat dari hash SHA-256 parameter: `videoId`, `song`, `artist`, `duration`, `isrc`.
   - Menjamin akurasi tinggi dan mencegah duplikasi data.
2. **Instant SSE Replay**:
   - Respon stream SSE (`event: metadata`, `event: provider`, dll.) di-buffer saat pertama kali di-fetch.
   - Pada request berikutnya dengan lagu yang sama, data langsung di-replay via HTTP chunked stream tanpa memanggil upstream.
   - Memangkas response time dari **500 - 2000 ms** menjadi **< 1 ms**.

---

## 7. Panduan Deployment Multi-Platform

### A. Koyeb
1. Deploy via Koyeb GitHub Integration atau Koyeb CLI.
2. Gunakan file konfigurasi yang sudah disediakan di [deployments/koyeb/koyeb.yaml](file:///home/container/betterlyrics-node/bete-node/deployments/koyeb/koyeb.yaml).
3. Set environment variable: `MODE=node` (atau `MODE=origin` jika ingin dijadikan Origin).

### B. Vercel
1. Hubungkan repository ke Vercel.
2. File [deployments/vercel/vercel.json](file:///home/container/betterlyrics-node/bete-node/deployments/vercel/vercel.json) dan [api/index.go](file:///home/container/betterlyrics-node/bete-node/api/index.go) sudah otomatis dikonfigurasi sebagai Go Serverless Function.
3. Tambahkan environment variables di Vercel Dashboard jika diperlukan.

### C. Netlify
1. Hubungkan repository ke Netlify.
2. Gunakan konfigurasi di [deployments/netlify/netlify.toml](file:///home/container/betterlyrics-node/bete-node/deployments/netlify/netlify.toml).

### D. Pterodactyl Panel
1. Import egg template: [deployments/pterodactyl/egg-bete-node.json](file:///home/container/betterlyrics-node/bete-node/deployments/pterodactyl/egg-bete-node.json).
2. Sistem otomatis mendeteksi path `/home/container`, membaca port dari `SERVER_PORT`, dan mengeksekusi startup wrapper [deployments/pterodactyl/start.sh](file:///home/container/betterlyrics-node/bete-node/deployments/pterodactyl/start.sh).

### E. VPS Linux (Systemd & Binary)
Jalankan 1-line auto installer:
```bash
sudo bash deployments/vps/install.sh
```
Atau jalankan manual:
```bash
# Build binary
make build
# Jalankan service
./bin/bete-node
```

### F. Docker & Docker Compose
```bash
cd deployments/vps
docker-compose up -d
```

---

## 8. Tabel Environment Variables

| Variabel | Tipe | Default | Deskripsi |
|---|---|---|---|
| `MODE` | string | `auto` | Mode operasi: `auto`, `origin`, atau `node`. |
| `ROLE` | string | `auto` | Alias untuk role: `origin` atau `node`. |
| `HOST` | string | `0.0.0.0` | Host listener. |
| `PORT` | string | `8080` (auto-detect) | Port aplikasi (otomatis membaca dari platform/Pterodactyl). |
| `UPSTREAM_URL` | string | `https://lyrics.api.dacubeking.com` | URL server resmi BetterLyrics. |
| `NODE1`, `NODE2`, ... | string | *(kosong)* | Endpoint edge node untuk Origin orchestrator. |
| `CACHE_TTL_SECONDS` | int | `259200` (3 hari) | Masa berlaku cache lirik dalam detik. |
| `CACHE_MAX_ITEMS` | int | `50000` | Kapasitas maksimal item dalam memory cache. |
| `AUTO_HEALTHCHECK` | bool | `true` | Mengaktifkan background health check berkala. |
| `HEALTHCHECK_INTERVAL_SEC` | int | `15` | Interval health check ke seluruh node (detik). |

---

## 9. Verifikasi & Endpoint API

| Endpoint | Method | Mode | Deskripsi |
|---|---|---|---|
| `/v2/lyrics` | `POST` | Origin & Node | Mengambil lirik dengan akselerasi cache & failover. |
| `/verify-turnstile` | `POST` | Origin & Node | Proxy token verifikasi Turnstile ke upstream. |
| `/interconnect` | `GET` | Node | Mengembalikan status kesehatan, resource, dan cache node. |
| `/nodes` atau `/status` | `GET` | Origin | Dashboard JSON untuk memantau seluruh node dan latensinya. |
| `/health` atau `/ping` | `GET` | Origin & Node | Endpoint ping ringan (return `OK`). |
