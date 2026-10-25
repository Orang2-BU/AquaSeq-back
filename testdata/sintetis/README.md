# Paket data SINTETIS AquaSeq

> **Bukan data tambak nyata.** Semua angka dibuat oleh `buat_data.py` untuk menguji alur AquaSeq dari unggah sampai keputusan. Data ini membuktikan pipeline berjalan, **bukan** akurasi model: skenarionya disusun sendiri, jadi wajar bila mesin "benar". Akurasi baru bisa diukur dengan data lapangan (validasi retrospektif dan uji lapangan).

## Isi

| Berkas | Isi |
|---|---|
| `qpcr/run-D14.csv` ... `run-D42.csv` | Satu run plate per tanggal sampling, format unggah web (`well,sample_code,target,ct,tm` + kurva standar, NTC) |
| `kimia-air.csv` | Kimia air pada hari sampling yang sama (suhu, pH, DO, TAN, NO2, H2S, TOM, alkalinitas, salinitas) |
| `kelimpahan/<kode>.csv` | Tabel kelimpahan taksa 16S per sampel (`taxon,genus,reads`) |
| `hasil-siklus.csv` | Hasil akhir siklus per kolam (`HARVEST_SUCCESS`, `EMERGENCY_HARVEST`, `OUTBREAK`) |
| `skenario.json` | Alur cerita dan harapan per kolam (dipakai tes Go `internal/sintetis`) |
| `buat_data.py` | Generator; hasil selalu sama (seed tetap) |

Tebar 1 September 2026; sampling DOC 14, 21, 28, 35, 42. Kode sampel `B2-D28` = kolam B2, DOC 28. Kolam B1-B4 sama dengan kolam demo di backend dan mock web, jadi sampel otomatis tercocokkan saat diunggah.

## Empat skenario

| Kolam | Cerita | Hasil mesin (V1-GSI) | Hasil siklus |
|---|---|---|---|
| B1 | Stabil sehat: tanpa patogen berarti, amoA tinggi, Vibrio < 6% | Kelas 1 di semua sampling | Panen normal |
| B2 | Memburuk pelan: pirA muncul DOC 21 lalu naik, amoA turun, TAN dan TOM naik, Vibrio 4% ke 26% | Kelas 1 → 2 (DOC 35) → 3 (DOC 42) | Panen dini DOC 48 |
| B3 | Wabah cepat: AHPND melonjak sejak DOC 21, EHP dan jejak WSSV menyusul, DO jatuh | Kelas 2 (DOC 21) → 4 (DOC 28 dan 35) | Wabah DOC 37, panen darurat |
| B4 | Tambak gambut: sampel DOC 21 terhambat inhibitor (PPC 24,6) | DOC 21 ditolak, lainnya kelas 1 | Panen normal |

## Memakai untuk demo web

1. Masuk sebagai pemilik, buka **Unggah data eDNA**, unggah `qpcr/run-D14.csv`, lanjut sampai langkah **Kolam & kimia air**, isi kimia air dari `kimia-air.csv` untuk kode sampel yang sama, lalu kirim. Ulangi untuk D21 sampai D42 secara berurutan.
2. Buka **Rincian skor** tiap sampel, unggah `kelimpahan/<kode>.csv` di kartu Komunitas mikroba (unggah dari DOC paling awal supaya baseline benar), lalu tekan **Tulis penjelasan ilmiah**.
3. Di **Detail kolam**, lihat tren peluang, porsi Vibrio per sampling, dan penanda mingguan. Catat hasil siklus dari `hasil-siklus.csv`.

## Memakai untuk tes dan uji penjelasan AI

- `go test ./internal/sintetis` memastikan cerita tiap kolam tetap sesuai mesin (log tes mencetak tabel kelas per sampel).
- `go run ./cmd/uji-penjelasan` menjalankan 25 kasus (19 sampel + 6 kasus tepi) memakai penulis dan pemilih dari env (`ANTHROPIC_API_KEY` atau `AI_*`, `JEV_API_KEY`) dan melaporkan berapa yang ditulis LLM dan lolos pemeriksa. Setiap panggilan LLM berbiaya.
