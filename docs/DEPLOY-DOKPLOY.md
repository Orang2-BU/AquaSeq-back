# Dokploy Packaging & Deployment Guide

Panduan konfigurasi dan deployment AquaSeq (Backend Go & Frontend Next.js) pada Dokploy VPS.

## 1. Ringkasan Arsitektur & Strategi Rilis

- **Backend:** Go 1.26 (Alpine multi-stage), port container `8080`, health check `GET /api/v1/health`.
- **Frontend:** Next.js 16 App Router (Node 22 standalone Alpine multi-stage), port container `3000`, health check `GET /api/health`.
- **Release Source:** Git source repository dibangun oleh Dockerfile di masing-masing repo.
- **Release Gate:** GitHub Actions memverifikasi seluruh build/test sebelum memicu Dokploy.
  - **PENTING: Nonaktifkan toggle "Auto Deploy" di Dokploy Dashboard.** Jika Auto Deploy aktif, Dokploy akan langsung me-rebuild saat push ke GitHub tanpa menunggu checks di CI selesai (bypass quality gates).
  - **Peringatan Branch Head Race:** Dokploy meng-clone `HEAD` branch pada saat proses build dimulai di VPS. Jika ada commit susulan sebelum rilis sebelumnya selesai, build dapat menarik kode yang belum selesai diuji di CI. Rilis diserialisasi via `concurrency` di GitHub Actions.

---

## 2. Pengaturan Dokploy Dashboard

### A. Backend (`aquaseq-back`)

| Pengaturan | Nilai | Catatan |
|---|---|---|
| **Repository** | `https://github.com/Orang2-BU/AquaSeq-back` | Branch rilis backend |
| **Branch** | `deployment` | Branch deployment backend |
| **Build Type** | `Dockerfile` | Multi-stage build Go 1.26 |
| **Dockerfile Path** | `Dockerfile` | Root repo |
| **Context Path** | `/` | Seluruh context dibutuhkan |
| **Container Port** | `8080` | Port aplikasi backend |
| **Auto Deploy** | **OFF (Nonaktif)** | Menghindari bypass CI checks |

#### Volume Persistent
- **Mount Path:** `/app/uploads`
- **Tipe:** Persistent Volume Dokploy / Docker named volume.
- **Keterangan:** Menyimpan file unggahan laporan tugas teknisi. Writable oleh user nonroot (UID 1001).

#### Health Check Probe
- **Path:** `/api/v1/health`
- **Method:** `GET`
- **Expected Status:** `200`
- **Body Response:** `{"success": true, "data": {"status": "healthy", ...}}`

#### Environment Variables (Backend)
| Variabel | Tipe | Contoh / Default | Keterangan |
|---|---|---|---|
| `PORT` | Public | `8080` | Port listener HTTP |
| `JWT_SECRET` | **Secret** | `(random 64-hex)` | **Wajib**. Minimal 16 karakter acak untuk enkripsi JWT |
| `DATABASE_URL` | **Secret** | `postgres://user:pass@dokploy-pg:5432/aquaseq?sslmode=disable` | Koneksi PostgreSQL. Jika kosong, fallback ke memory store |
| `DATABASE_MAX_CONNS` | Config | `20` | Pool koneksi database |
| `FILES_DIR` | Config | `/app/uploads` | Lokasi upload file di container |
| `WEB_URL` | Config | `https://aquaseq.example.com` | URL publik frontend untuk tautan reset sandi & undangan |
| `DEMO_ENABLED` | Config | `true` | Mengizinkan login akun demo simulasi |
| `LLM_PROVIDER` | Config | `jev` | Opsional: provider AI (TypeSafe Jev) |
| `JEV_API_KEY` | **Secret** | `(secret key)` | Opsional: API key TypeSafe AI |
| `JEV_BASE_URL` | Config | `https://api.typesafe.ai` | Opsional: URL API Jev |
| `JEV_MODEL` | Config | `jev-latest` | Opsional: Nama model AI |
| `ANTHROPIC_API_KEY` | **Secret** | `(secret key)` | Opsional: penulis penjelasan ilmiah memakai Claude lewat SDK resmi. Bila diisi, dipakai lebih dulu daripada `LLM_PROVIDER=openai` |
| `ANTHROPIC_MODEL` | Config | `claude-opus-5-5` | Opsional: model Claude penulis penjelasan (bawaan `claude-opus-5-5`) |
| `ANTHROPIC_BASE_URL` | Config | `https://api.anthropic.com` | Opsional: hanya bila memakai gateway yang kompatibel dengan API Anthropic |
| `SMTP_HOST` | Config | `smtp.example.com` | Opsional: Host SMTP notifikasi email |
| `SMTP_PORT` | Config | `587` | Opsional: Port SMTP |
| `SMTP_USER` | Config | `noreply@example.com` | Opsional: Username SMTP |
| `SMTP_PASS` | **Secret** | `(smtp password)` | Opsional: Password SMTP |
| `SMTP_FROM` | Config | `AquaSeq <noreply@example.com>` | Opsional: Pengirim email |

*Catatan Migrasi SQL:* `internal/repository/postgres.go` membaca `sql/schema.sql` di filesystem saat startup. File ini telah disalin ke image produksi di `/app/sql/schema.sql`.

---

### B. Frontend (`aquaseq-web` / `aquaseq-front`)

| Pengaturan | Nilai | Catatan |
|---|---|---|
| **Repository** | `https://github.com/Orang2-BU/AquaSeq-front` | Branch rilis frontend |
| **Branch** | `push-deployment` | Branch deployment frontend |
| **Build Type** | `Dockerfile` | Multi-stage Node 22 standalone |
| **Dockerfile Path** | `Dockerfile` | Root repo |
| **Context Path** | `/` | Root context |
| **Container Port** | `3000` | Port container Next.js standalone |
| **Auto Deploy** | **OFF (Nonaktif)** | Menghindari bypass CI checks |

#### Health Check Probe
- **Path:** `/api/health`
- **Method:** `GET`
- **Expected Status:** `200`

#### Environment Variables (Frontend)
| Variabel | Tipe | Contoh / Default | Keterangan |
|---|---|---|---|
| `PORT` | Public | `3000` | Port listener Next.js |
| `HOSTNAME` | Public | `0.0.0.0` | Binding IP |
| `NODE_ENV` | Public | `production` | Mode produksi |
| `NEXT_TELEMETRY_DISABLED` | Public | `1` | Mematikan telemetri Next.js |
| `API_MODE` | Config | `live` (atau `mock`) | `live` untuk memanggil Go backend; `mock` untuk data simulasi |
| `API_BASE_URL` | Config | `http://dokploy-backend:8080` | URL internal/eksternal backend Go (hanya diakses via server Next.js) |

---

## 3. Konfigurasi CI/CD di GitHub Actions

Workflow `.github/workflows/deploy.yml` dikonfigurasi dengan prinsip least-privilege dan safety guards:

### A. Guard Flag `DOKPLOY_DEPLOY_ENABLED`
Karena VPS belum tentu dikonfigurasi oleh pemilik repositori saat kode di-push:
- Tambahkan Repository Variable di GitHub:
  - Masuk ke repo GitHub: **Settings -> Secrets and variables -> Actions -> Variables tab**.
  - Buat variable baru: `DOKPLOY_DEPLOY_ENABLED` dengan nilai `false` (default) atau `true`.
- Jika `DOKPLOY_DEPLOY_ENABLED` bernilai `false` (atau belum diset), job deploy akan **otomatis di-skip** tanpa error.
- Jika `DOKPLOY_DEPLOY_ENABLED` bernilai `true`, job deploy wajib dijalankan, dan jika secret yang dibutuhkan tidak tersedia, job akan **gagal (fail-fast)** dan tidak memberikan klaim sukses palsu.

### B. Dokploy API Trigger (Metode Utama yang Direkomendasikan)
Metode ini bekerja saat **Auto Deploy Dokploy dimatikan**, sehingga hanya rilis yang telah lulus seluruh pengujian CI yang di-deploy.

Secrets yang wajib diisi di GitHub (**Settings -> Secrets and variables -> Actions -> Secrets tab**):
1. `DOKPLOY_URL`: URL instance Dokploy Anda (contoh: `https://dokploy.domainanda.com`). Wajib berupa HTTPS origin bersih tanpa path, query, atau credential.
2. `DOKPLOY_API_KEY`: API token Dokploy. Dibuat di dashboard Dokploy: **Profile Settings -> API/CLI -> Generate Token**.
3. `DOKPLOY_APPLICATION_ID`: ID aplikasi target di Dokploy. Diperoleh via API `GET /api/project.all` menggunakan token Dokploy.

### C. Dokploy Webhook URL (Metode Alternatif)
Dokploy juga menyediakan endpoint Webhook per aplikasi. Jika menggunakan metode webhook:
- Secret `DOKPLOY_WEBHOOK_URL` tetap didukung dan tidak dihapus/diinvalidasi.
- **PENTING:** Handler webhook Dokploy untuk provider GitHub mengharuskan request menyertakan informasi Git payload:
  ```json
  {
    "ref": "refs/heads/deployment",
    "head_commit": {
      "id": "<GITHUB_SHA>",
      "message": "<COMMIT_MESSAGE>"
    },
    "repository": {
      "name": "<REPO_NAME>"
    }
  }
  ```
  Serta mengharuskan toggle **Auto Deploy diaktifkan** di aplikasi Dokploy terkait.
- Header `Content-Type: application/json` wajib disertakan.
- Respons dari webhook hanya menandakan trigger diterima Dokploy dalam antrean (queued), **bukan bukti bahwa rollout dan health check container telah selesai**.

---

## 4. Status Verifikasi (Tested vs Docs)

| Layer Verifikasi | Backend (`AquaSeq-back`) | Frontend (`AquaSeq-front`) | Catatan |
|---|---|---|---|
| **Dockerfile Multi-Stage Build** | ✅ **Tested & Verified** | 📖 **Documented in CI** | Backend berhasil di-build (`aquaseq-back:local-check`). Frontend diserahkan ke pipeline CI karena perubahan aktif agent lain |
| **Nonroot User & Uploads Vol** | ✅ **Tested & Verified** | ✅ **Tested & Verified** | Backend: UID 1001 `app:app`, `FILES_DIR=/app/uploads`. Frontend: UID 1001 `nextjs:nodejs` |
| **SQL Schema Ingestion** | ✅ **Tested & Verified** | N/A | `/app/sql/schema.sql` disalin ke image runner untuk mendukung `autoMigrate` tanpa error |
| **Local Container Health Probe** | ✅ **Tested & Verified** | 📖 **Documented** | Backend container berjalan di `127.0.0.1:18081`, merespons `200 OK` pada `GET /api/v1/health` |
| **GitHub Actions Pipeline** | ✅ **Configured** | ✅ **Configured** | Branch guards (`deployment` & `push-deployment`), safe JSON payload, fail-fast pada enabled |
| **Dokploy VPS Rollout** | ⛔ **Unverified (No VPS)** | ⛔ **Unverified (No VPS)** | Belum ada VPS Dokploy yang terhubung (default `DOKPLOY_DEPLOY_ENABLED=false`) |

### Penjelasan ilmiah (Opus + Jev)
- Penulis: `ANTHROPIC_API_KEY` (Claude Opus via SDK resmi) atau, bila kosong, `LLM_PROVIDER=openai` + `AI_API_KEY`/`AI_BASE_URL`/`AI_MODEL` (gateway kompatibel OpenAI). Pemilih tindakan: `JEV_API_KEY` (tidak bergantung `LLM_PROVIDER`).
- Tanpa kunci apa pun, endpoint tetap jalan dengan penjelasan template. Tidak perlu migrasi manual: tabel `sample_explanations` dan `sample_microbiome` dibuat otomatis dari `sql/schema.sql` saat server menyala.
- Batas waktu penulis 22 detik (di bawah `WriteTimeout` 30 detik). Bila habis atau jawaban gagal diperiksa, hasilnya template dan alasannya tercatat di `fallback_reason`.
- Model Claude menolak parameter `temperature` sejak 21 Okt 2026; klien kompatibel OpenAI tidak lagi mengirimnya untuk model yang namanya mengandung `claude`.
