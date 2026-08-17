# 🎵 BetterLyrics Bete-Node (Origin & Edge Accelerator)

High-performance multi-node accelerator and origin orchestrator written in Go for [BetterLyrics](https://betterlyrics.org).

---

## ⚡ Key Highlights
- **Multi-Platform Native Support**: Koyeb, Vercel, Netlify, Pterodactyl (`/home/container`), VPS (systemd/binary), and Docker.
- **Dynamic Node Management**: Add nodes dynamically via `.env` (`NODE1=...`, `NODE2=...`, etc.) without rebuilding.
- **Smart Latency Routing & Healthcheck**: Origin pings all edge nodes and routes traffic to the fastest alive node.
- **Automatic Fallback & Circuit Breaker**: If all nodes fail, fallback seamlessly to official upstream.
- **Zero-Latency In-Memory Cache**: Cached lyrics are streamed back in **< 1ms**.
- **Auto Environment Discovery**: Automatically locates `.env` in `/home/container/bete-node/`, `$PWD/bete-node/`, next to binary, or OS exports.

---

## 🚀 Quick Start

### 1. Run via Go
```bash
cd bete-node
cp .env.example .env
go run ./cmd/app
```

### 2. Build Binary
```bash
make build-all
# Output available in bin/
```

### 3. Change Mode
- **Origin Mode**: Set `MODE=origin` or provide `NODE1=...` in `.env`.
- **Edge Node Mode**: Set `MODE=node` (provide this URL to Origin's `NODE[N]`).

---

## 🌐 Connecting Nodes to Origin
Di dalam file `.env` Origin:
```env
MODE=origin
PORT=8080
UPSTREAM_URL=https://lyrics.api.dacubeking.com

# Tambahkan Edge Node Anda:
NODE1=https://node1.koyeb.app/interconnect
NODE2=https://my-node.vercel.app/interconnect
NODE3=node3.vps.com/interconnect
```

Buka `http://localhost:8080/nodes` untuk melihat dashboard status seluruh node secara real-time.
