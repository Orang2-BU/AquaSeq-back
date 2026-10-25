"""Generator paket data SINTETIS AquaSeq (bukan data tambak nyata).

Membuat 4 kolam (B1-B4) x sampling DOC 14/21/28/35/42 dalam satu siklus:
  qpcr/run-DXX.csv      satu run plate per tanggal sampling (format unggah web: well,sample_code,target,ct,tm + kurva standar)
  kimia-air.csv         kimia air yang dicatat pada hari sampling yang sama
  kelimpahan/<kode>.csv tabel kelimpahan taksa 16S per sampel (taxon,genus,reads)
  hasil-siklus.csv      hasil akhir siklus per kolam
  skenario.json         alur cerita dan harapan per kolam (dipakai tes Go)

Jalankan ulang: python testdata/sintetis/buat_data.py  (hasil selalu sama, seed tetap).
"""
import csv, json, math, os, random
from datetime import date, timedelta

HERE = os.path.dirname(os.path.abspath(__file__))
START = date(2026, 9, 1)  # hari tebar (DOC 0)
DAYS = [14, 21, 28, 35, 42]
rng = random.Random(20261025)

# Ct per target per kolam per DOC. None = Undetermined (tidak terdeteksi). PPC = kontrol proses.
QPCR = {
    "B1": {  # stabil sehat
        14: dict(pirA=None, vp28=None, swp1=None, amoA=22.1, s16=18.2, ppc=19.9),
        21: dict(pirA=None, vp28=None, swp1=None, amoA=22.4, s16=18.1, ppc=20.0),
        28: dict(pirA=None, vp28=None, swp1=None, amoA=22.0, s16=18.3, ppc=19.8),
        35: dict(pirA=35.6, vp28=None, swp1=None, amoA=22.6, s16=18.2, ppc=20.1),
        42: dict(pirA=None, vp28=None, swp1=None, amoA=22.3, s16=18.2, ppc=19.9),
    },
    "B2": {  # memburuk pelan: pirA muncul lalu naik, bakteri nitrifikasi turun
        14: dict(pirA=None, vp28=None, swp1=None, amoA=22.6, s16=18.2, ppc=20.0),
        21: dict(pirA=34.8, vp28=None, swp1=None, amoA=23.8, s16=18.1, ppc=19.9),
        28: dict(pirA=31.4, vp28=None, swp1=None, amoA=25.2, s16=18.3, ppc=20.2),
        35: dict(pirA=28.5, vp28=None, swp1=None, amoA=26.6, s16=18.2, ppc=19.8),
        42: dict(pirA=26.1, vp28=None, swp1=None, amoA=27.9, s16=18.4, ppc=20.1),
    },
    "B3": {  # wabah cepat: AHPND melonjak, disusul EHP dan jejak WSSV; panen darurat DOC 37
        14: dict(pirA=None, vp28=None, swp1=None, amoA=22.8, s16=18.2, ppc=19.9),
        21: dict(pirA=27.4, vp28=None, swp1=None, amoA=24.9, s16=18.3, ppc=20.0),
        28: dict(pirA=23.3, vp28=None, swp1=30.1, amoA=27.6, s16=18.2, ppc=20.1),
        35: dict(pirA=20.7, vp28=33.2, swp1=27.0, amoA=29.8, s16=18.4, ppc=20.2),
    },
    "B4": {  # tambak gambut: DOC 21 terhambat inhibitor (PPC 24,6), lainnya ringan
        14: dict(pirA=None, vp28=None, swp1=None, amoA=23.0, s16=18.3, ppc=20.4),
        21: dict(pirA=33.9, vp28=None, swp1=None, amoA=26.4, s16=18.2, ppc=24.6),
        28: dict(pirA=33.6, vp28=None, swp1=None, amoA=23.4, s16=18.2, ppc=20.6),
        35: dict(pirA=None, vp28=None, swp1=None, amoA=23.1, s16=18.3, ppc=20.3),
        42: dict(pirA=34.9, vp28=None, swp1=None, amoA=23.5, s16=18.1, ppc=20.5),
    },
}

# temp_c, ph, do_mg_l, tan_mg_l, no2_mg_l, h2s_mg_l, tom_mg_l, alkalinity, salinity_ppt
WATER = {
    "B1": {14: (29.4, 7.9, 6.0, 0.32, 0.15, 0.002, 52, 148, 20), 21: (29.8, 7.9, 5.8, 0.38, 0.18, 0.002, 55, 146, 20),
           28: (30.1, 8.0, 5.7, 0.41, 0.22, 0.003, 58, 144, 21), 35: (30.0, 8.0, 5.6, 0.45, 0.25, 0.003, 61, 142, 21),
           42: (29.7, 7.9, 5.7, 0.44, 0.27, 0.003, 63, 141, 21)},
    "B2": {14: (29.6, 8.0, 5.7, 0.40, 0.28, 0.003, 60, 140, 21), 21: (30.0, 8.1, 5.3, 0.62, 0.41, 0.004, 66, 134, 21),
           28: (30.3, 8.2, 4.9, 0.94, 0.58, 0.006, 73, 126, 22), 35: (30.5, 8.3, 4.5, 1.28, 0.77, 0.008, 80, 118, 22),
           42: (30.6, 8.3, 4.3, 1.62, 0.93, 0.009, 86, 112, 22)},
    "B3": {14: (29.9, 8.0, 5.6, 0.48, 0.30, 0.003, 62, 138, 21), 21: (30.6, 8.2, 4.6, 1.05, 0.88, 0.007, 82, 120, 22),
           28: (31.0, 8.4, 3.8, 2.20, 1.64, 0.013, 104, 101, 22), 35: (31.2, 8.5, 3.1, 3.40, 2.85, 0.031, 129, 88, 23)},
    "B4": {14: (28.9, 7.5, 5.9, 0.36, 0.20, 0.002, 70, 104, 14), 21: (29.1, 7.4, 5.6, 0.42, 0.24, 0.003, 76, 98, 14),
           28: (29.3, 7.5, 5.5, 0.44, 0.26, 0.003, 74, 101, 15), 35: (29.4, 7.6, 5.6, 0.47, 0.28, 0.003, 72, 106, 15),
           42: (29.2, 7.6, 5.7, 0.46, 0.27, 0.003, 71, 108, 15)},
}

# Porsi Vibrio (%) dan tingkat dominasi komunitas (1 = beragam, makin besar makin didominasi).
MICRO = {
    "B1": {14: (3.8, 1.0), 21: (4.1, 1.0), 28: (4.6, 1.05), 35: (5.2, 1.05), 42: (4.9, 1.05)},
    "B2": {14: (4.4, 1.0), 21: (7.9, 1.15), 28: (12.6, 1.35), 35: (19.4, 1.6), 42: (26.8, 1.85)},
    "B3": {14: (5.1, 1.0), 21: (17.2, 1.5), 28: (33.5, 2.1), 35: (48.9, 2.7)},
    "B4": {14: (3.2, 1.0), 21: (4.0, 1.05), 28: (4.5, 1.05), 35: (3.9, 1.0), 42: (4.7, 1.05)},
}

OUTCOMES = [
    ("B1", 1, "HARVEST_SUCCESS", 1840, "", "", "Panen normal DOC 95, SR baik."),
    ("B2", 1, "EMERGENCY_HARVEST", 920, "", "AHPND", "Panen dini DOC 48 setelah jejak AHPND terus naik dan DO turun."),
    ("B3", 1, "OUTBREAK", 210, "2026-10-08", "AHPND", "Kematian massal DOC 37; panen darurat."),
    ("B4", 1, "HARVEST_SUCCESS", 1510, "", "", "Panen normal; sampel DOC 21 sempat ditolak karena inhibitor gambut."),
]

STORY = {
    "B1": "Stabil sehat: tanpa patogen berarti, amoA tinggi, kimia air aman, Vibrio < 6%.",
    "B2": "Memburuk pelan: jejak pirA muncul DOC 21 lalu naik, amoA turun, TAN dan TOM naik, Vibrio 4% ke 27%.",
    "B3": "Wabah cepat: AHPND melonjak sejak DOC 21, EHP dan jejak WSSV menyusul, DO jatuh, panen darurat DOC 37.",
    "B4": "Tambak gambut: sampel DOC 21 terhambat inhibitor (PPC 24,6) sehingga ditolak, sampel lain aman.",
}

STANDARDS = [("STD-1", 1000000, 19.80), ("STD-2", 100000, 23.20), ("STD-3", 10000, 26.50), ("STD-4", 1000, 29.90), ("STD-5", 100, 33.30)]
TM = {"pirA": 81.2, "vp28": 79.6, "swp1": 83.4, "amoA": 84.1, "16S": 86.3, "PPC": 82.7}

TAXA = [
    ("Rhodobacter sphaeroides", "Rhodobacter", 9), ("Bacillus subtilis", "Bacillus", 8), ("Bacillus licheniformis", "Bacillus", 5),
    ("Nitrosomonas europaea", "Nitrosomonas", 6), ("Nitrobacter winogradskyi", "Nitrobacter", 4), ("Pseudoalteromonas sp.", "Pseudoalteromonas", 6),
    ("Ruegeria pomeroyi", "Ruegeria", 5), ("Marinobacter hydrocarbonoclasticus", "Marinobacter", 4), ("Halomonas sp.", "Halomonas", 4),
    ("Flavobacterium sp.", "Flavobacterium", 5), ("Tenacibaculum sp.", "Tenacibaculum", 3), ("Shewanella algae", "Shewanella", 3),
    ("Photobacterium damselae", "Photobacterium", 2), ("Synechococcus sp.", "Synechococcus", 5), ("Erythrobacter sp.", "Erythrobacter", 3),
    ("Roseobacter sp.", "Roseobacter", 4), ("Lactobacillus plantarum", "Lactobacillus", 2), ("Exiguobacterium sp.", "Exiguobacterium", 2),
    ("Alteromonas macleodii", "Alteromonas", 3), ("Thalassospira sp.", "Thalassospira", 2), ("Sulfitobacter sp.", "Sulfitobacter", 2),
    ("Paracoccus sp.", "Paracoccus", 2), ("Microbacterium sp.", "Microbacterium", 1.5), ("Planctomyces sp.", "Planctomyces", 1.5),
]
VIBRIO = [("Vibrio harveyi", 0.45), ("Vibrio parahaemolyticus", 0.35), ("Vibrio alginolyticus", 0.20)]
RARE = ["Kordiimonas sp.", "Muricauda sp.", "Pelagibius sp.", "Hyphomonas sp.", "Labrenzia sp.", "Oceanicola sp.", "Maribius sp.", "Nisaea sp."]


def code(pond, day):
    return f"{pond}-D{day}"


def sampling_date(day):
    return (START + timedelta(days=day)).isoformat()


def fmt_ct(v):
    return "Undetermined" if v is None else f"{v + rng.uniform(-0.12, 0.12):.2f}"


def write_qpcr():
    os.makedirs(os.path.join(HERE, "qpcr"), exist_ok=True)
    cols = "ABCDEFGH"
    for day in DAYS:
        wells = []
        idx = 0

        def well():
            nonlocal idx
            w = f"{cols[idx % 8]}{idx // 8 + 1}"
            idx += 1
            return w

        for pond in sorted(QPCR):
            if day not in QPCR[pond]:
                continue
            q = QPCR[pond][day]
            sc = code(pond, day)
            for _ in range(2):  # pirA duplo
                wells.append((well(), sc, "pirA", fmt_ct(q["pirA"]), TM["pirA"] if q["pirA"] else ""))
            for target, key in (("vp28", "vp28"), ("swp1", "swp1"), ("amoA", "amoA"), ("16S", "s16"), ("PPC", "ppc")):
                v = q[key]
                ct = "Undetermined" if v is None else (f"{v:.2f}" if target == "PPC" else fmt_ct(v))
                wells.append((well(), sc, target, ct, TM[target] if v else ""))
        for target in ("swp1", "vp28", "pirA"):
            wells.append((f"H{10 + ('swp1', 'vp28', 'pirA').index(target)}", "NTC", target, "Undetermined", ""))
        path = os.path.join(HERE, "qpcr", f"run-D{day}.csv")
        with open(path, "w", newline="", encoding="utf-8") as f:
            w = csv.writer(f, lineterminator="\n")
            w.writerow(["well", "sample_code", "target", "ct", "tm"])
            w.writerows(wells)
            w.writerow([])
            w.writerow(["std_level", "copies_rxn", "ct"])
            for s in STANDARDS:
                w.writerow([s[0], s[1], f"{s[2]:.2f}"])


def write_water():
    with open(os.path.join(HERE, "kimia-air.csv"), "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f, lineterminator="\n")
        w.writerow(["sample_code", "pond_code", "cycle_day", "sampling_date", "temp_c", "ph", "do_mg_l", "tan_mg_l",
                    "no2_mg_l", "h2s_mg_l", "tom_mg_l", "alkalinity", "salinity_ppt"])
        for pond in sorted(WATER):
            for day, v in sorted(WATER[pond].items()):
                w.writerow([code(pond, day), pond, day, sampling_date(day), *v])


def abundance(vibrio_pct, dominance, total):
    weights = [(t, g, base ** dominance) for t, g, base in TAXA]
    # Komunitas yang makin didominasi kehilangan takson berbobot kecil.
    if dominance > 1.4:
        weights = weights[: max(10, int(len(weights) / dominance * 1.2))]
    other = total * (1 - vibrio_pct / 100)
    s = sum(x[2] for x in weights)
    rows = []
    for t, g, wgt in weights:
        rows.append((t, g, max(3, round(other * wgt / s * rng.uniform(0.85, 1.15)))))
    vib_total = total * vibrio_pct / 100
    for t, share in VIBRIO:
        rows.append((t, "Vibrio", max(1, round(vib_total * share * rng.uniform(0.9, 1.1)))))
    n_rare = 6 if dominance < 1.3 else 3 if dominance < 2 else 1
    for name in rng.sample(RARE, n_rare):
        rows.append((name, name.split()[0], rng.choice([1, 1, 2])))
    return rows


def write_abundance():
    os.makedirs(os.path.join(HERE, "kelimpahan"), exist_ok=True)
    for pond in sorted(MICRO):
        for day, (vib, dom) in sorted(MICRO[pond].items()):
            total = rng.randint(18000, 26000)
            path = os.path.join(HERE, "kelimpahan", f"{code(pond, day)}.csv")
            with open(path, "w", newline="", encoding="utf-8") as f:
                w = csv.writer(f, lineterminator="\n")
                w.writerow(["taxon", "genus", "reads"])
                w.writerows(abundance(vib, dom, total))


def write_outcomes():
    with open(os.path.join(HERE, "hasil-siklus.csv"), "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f, lineterminator="\n")
        w.writerow(["pond_code", "cycle_number", "outcome", "actual_harvest_kg", "outbreak_date", "primary_suspect_pathogen", "notes"])
        w.writerows(OUTCOMES)


def write_scenario():
    data = {
        "label": "DATA SINTETIS - bukan data tambak nyata. Hanya untuk menguji alur, bukan akurasi model.",
        "stocking_date": START.isoformat(),
        "ponds": {
            p: {
                "story": STORY[p],
                "samples": [{"sample_code": code(p, d), "cycle_day": d, "sampling_date": sampling_date(d)} for d in sorted(QPCR[p])],
                "outcome": next(o[2] for o in OUTCOMES if o[0] == p),
            }
            for p in sorted(QPCR)
        },
        "expect": {
            "rejected": ["B4-D21"],
            "worst_class_at_least": {"B3": 4, "B2": 2},
            "max_class_at_most": {"B1": 2, "B4": 2},
            "vibrio_rising": ["B2", "B3"],
        },
    }
    with open(os.path.join(HERE, "skenario.json"), "w", encoding="utf-8", newline="\n") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")


if __name__ == "__main__":
    write_qpcr()
    write_water()
    write_abundance()
    write_outcomes()
    write_scenario()
    print("selesai:", HERE)
