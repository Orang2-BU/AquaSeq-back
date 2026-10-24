# Safety & Governance Protocol (AquaSeq AI)

Dokumen ini mendefinisikan batasan keselamatan, etika operasional, dan aturan peninjauan internal untuk sistem bantuan analitik AquaSeq. Dokumen ini BUKAN prosedur operasional standar (SOP) klinis tervalidasi secara eksternal oleh otoritas veteriner atau medis akuakultur resmi, melainkan panduan verifikasi internal rekayasa perangkat lunak dan manajemen risiko tambak.

---

<a id="review"></a>
## #review: Aturan Peninjauan Lapangan dan Batasan Medis

1. **Bukan Prosedur Klinis Tervalidasi Eksternal**:
   - Model analitik AquaSeq (termasuk TypeSafe System One / Jev Client dan skoring SDRI) adalah alat bantu penyaringan statistik (*decision-support screening*).
   - Seluruh hasil inferensi dan skor probabilitas **tidak boleh diklaim sebagai diagnosis penyakit yang memiliki akurasi klinis tervalidasi**.

2. **Larangan Peresepan Otomatis, Dosis, dan Antibiotik**:
   - **TIDAK ADA PERESEPAN OTOMATIS**: Sistem tidak boleh menerbitkan resep otomatis. Keputusan terapi berada di luar kewenangan modul analitik.
   - **TIDAK ADA REKOMENDASI DOSIS**: Sistem dilarang menghitung atau menyarankan dosis bahan kimia aktif atau obat-obatan.
   - **DILARANG MENYARANKAN ANTIBIOTIK**: Modul analitik tidak boleh merekomendasikan antibiotik. Ini batasan produk internal, bukan klaim standar klinis eksternal.
   - Saran tindakan hanya untuk peninjauan manusia, bukan instruksi eksekusi otomatis di tambak.

3. **Kewajiban Konsultasi Profesional**:
   - Setiap anomali atau kenaikan kelas risiko wajib ditinjau bersama teknisi laboratorium akuakultur dan ahli patologi veteriner bersertifikat sebelum keputusan manajemen tambak diambil.

---

<a id="verification"></a>
## #verification: Verifikasi Integritas Data dan Sampel Sumber

1. **Integritas Identitas Sampel**:
   - Setiap sampel harus memiliki kode identitas (*sample_code*) unik dan diverifikasi kesesuaiannya dengan dokumen rantai kepemilikan (*chain of custody*) laboratorium sumber.
   - Validasi kecocokan sampel berbasis model (misal *noul* match) adalah verifikasi konsistensi teks dokumen, bukan jaminan biologis asal sampel.

2. **Kontrol Kualitas Laboratorium (QC & NTC)**:
   - Peninjau harus memeriksa kontrol positif, kontrol negatif, status QC, dan pencilan pada laporan laboratorium sumber. QC gagal, kontaminasi, atau bukti QC tidak lengkap harus diselesaikan bersama laboratorium sebelum keputusan lapangan; jangan menganggap data hilang sebagai hasil negatif.

3. **Kelengkapan Parameter Lingkungan**:
   - Peninjau harus memeriksa kelengkapan kimia air yang relevan, satuan, waktu pengukuran, dan asal data terhadap dokumen sumber. Nilai yang tidak tersedia harus tetap ditandai tidak tersedia.
   - Dilarang menginterpolasi atau mengarang data kualitas air yang tidak tercatat pada dokumen sumber primer.
