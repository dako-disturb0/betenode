# 📦 Standalone Binaries (`binary/`)

Daftar biner executable siap pakai untuk berbagai arsitektur OS/CPU tanpa memerlukan instalasi Go:

| File Biner | Target Arsitektur / CPU | Contoh Penggunaan Platform |
|---|---|---|
| [`bete-node-linux-amd64`](file:///home/container/betterlyrics-node/bete-node/binary/bete-node-linux-amd64) | **Linux AMD64 / x86_64** (64-bit) | VPS (Ubuntu/Debian/CentOS), Pterodactyl, Docker, Koyeb, AWS EC2, DigitalOcean |
| [`bete-node-linux-arm64`](file:///home/container/betterlyrics-node/bete-node/binary/bete-node-linux-arm64) | **Linux ARM64 / AArch64** (64-bit) | Oracle Cloud ARM (Ampere), Raspberry Pi 4/5 (64-bit), Apple Silicon Linux VM |
| [`bete-node-linux-x86`](file:///home/container/betterlyrics-node/bete-node/binary/bete-node-linux-x86) | **Linux x86 / i386** (32-bit) | VPS / Server Linux Legacy (32-bit) |
| [`bete-node-linux-armv7`](file:///home/container/betterlyrics-node/bete-node/binary/bete-node-linux-armv7) | **Linux ARMv7 / ARM32** | Raspberry Pi 2/3 (32-bit OS), Embedded Linux |
| [`bete-node-windows-amd64.exe`](file:///home/container/betterlyrics-node/bete-node/binary/bete-node-windows-amd64.exe) | **Windows x64** | Windows 10/11 / Windows Server |

---

## 🚀 Cara Menjalankan:

### Linux (Semua Arsitektur):
```bash
# 1. Berikan hak akses eksekusi
chmod +x ./binary/bete-node-linux-amd64

# 2. Jalankan binary
./binary/bete-node-linux-amd64

# Atau dengan flag kustom:
./binary/bete-node-linux-amd64 -mode=node -port=8080
```

### Windows:
```cmd
bete-node-windows-amd64.exe -mode=node -port=8080
```
