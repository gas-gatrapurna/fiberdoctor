package zte

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenDirs memetakan key OLT ke direktori fixture sintetis.
//
// Fixture di testdata/zte_c320_synth/ berisi potongan output CLI ZTE C320
// yang DISINTESIS: serial number, nama pelanggan, dan lokasi semuanya fiktif.
// Fixture sengaja di-commit agar golden test tetap berjalan di repo publik
// tanpa akses ke perangkat fisik.
//
// Untuk membuat ground-truth dari perangkat sendiri, pakai cmd/fd_golden
// (read-only, hanya perintah "show ") dan simpan keluarannya DI LUAR repo.
var goldenDirs = map[string]string{
	"OLT-SYNTH-A": "../../../testdata/zte_c320_synth/olt-synth-a",
	"OLT-SYNTH-B": "../../../testdata/zte_c320_synth/olt-synth-b",
}

// goldenFile membaca fixture golden. Fixture bersifat WAJIB: bila hilang,
// test GAGAL (bukan skip) supaya regresi tidak lolos diam-diam.
func goldenFile(t *testing.T, olt, name string) string {
	t.Helper()
	dir, ok := goldenDirs[olt]
	if !ok {
		t.Fatalf("OLT %q tidak dikenal untuk golden test", olt)
	}
	path := filepath.Join(dir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("gagal membaca golden file %s: %v", path, err)
	}
	return string(raw)
}

// TestGoldenONUState memvalidasi parser terhadap tabel state CLI sintetis.
//
// Karena fixture bersifat deterministik, distribusi status di-assert dengan
// angka absolut (bukan rentang toleransi seperti pada data live).
func TestGoldenONUState(t *testing.T) {
	cases := []struct {
		olt    string
		total  int
		counts map[string]int
	}{
		{
			olt:   "OLT-SYNTH-A",
			total: 8,
			counts: map[string]int{
				"Online":    5,
				"Offline":   1,
				"LOS":       1,
				"DyingGasp": 1,
			},
		},
		{
			olt:   "OLT-SYNTH-B",
			total: 8,
			counts: map[string]int{
				"Online":    4,
				"Offline":   2,
				"LOS":       1,
				"DyingGasp": 1,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.olt, func(t *testing.T) {
			records, err := ParseZTEONUState(goldenFile(t, tc.olt, "gpon_onu_state.txt"), tc.olt)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(records) != tc.total {
				t.Fatalf("expected %d ONU, got %d", tc.total, len(records))
			}

			counts := map[string]int{}
			for _, rec := range records {
				counts[rec.Status]++
				if rec.AdminState == "" || rec.OMCCState == "" || rec.PhaseState == "" || rec.Channel == "" {
					t.Fatalf("ONU %s kehilangan kolom state: %+v", rec.ONUID, rec)
				}
			}

			for status, want := range tc.counts {
				if counts[status] != want {
					t.Errorf("status %s = %d, want %d (distribusi: %v)",
						status, counts[status], want, counts)
				}
			}
		})
	}
}

// TestGoldenRegistry memvalidasi pemetaan SN dari running-config sintetis.
func TestGoldenRegistry(t *testing.T) {
	cases := []struct {
		olt   string
		total int
	}{
		{olt: "OLT-SYNTH-A", total: 8},
		{olt: "OLT-SYNTH-B", total: 8},
	}

	for _, tc := range cases {
		t.Run(tc.olt, func(t *testing.T) {
			registry := ParseZTERegistry(goldenFile(t, tc.olt, "running_config.txt"))
			if len(registry) != tc.total {
				t.Fatalf("expected %d ONU terdaftar, got %d", tc.total, len(registry))
			}
			for key, entry := range registry {
				if entry.SN == "" || len(entry.SN) < 10 {
					t.Errorf("ONU %s punya SN tidak wajar: %q", key, entry.SN)
				}
			}
		})
	}
}

// TestGoldenStateAndRegistryInvariant memastikan setiap ONU pada tabel state
// juga terdaftar (punya SN) di running-config - invarian inventaris.
func TestGoldenStateAndRegistryInvariant(t *testing.T) {
	for olt := range goldenDirs {
		t.Run(olt, func(t *testing.T) {
			stateRecords, err := ParseZTEONUState(goldenFile(t, olt, "gpon_onu_state.txt"), olt)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			registry := ParseZTERegistry(goldenFile(t, olt, "running_config.txt"))

			missing := 0
			for _, rec := range stateRecords {
				if _, ok := registry[rec.ONUID]; !ok {
					missing++
					if missing <= 5 {
						t.Errorf("ONU %s ada di state tapi tidak di running-config", rec.ONUID)
					}
				}
			}
			if missing > 0 {
				t.Errorf("total %d ONU tidak punya entri running-config", missing)
			}
		})
	}
}

// TestGoldenRunningConfigONUs memvalidasi jumlah interface ONU & sebaran VLAN.
func TestGoldenRunningConfigONUs(t *testing.T) {
	cases := []struct {
		olt    string
		total  int
		expect map[int]int
	}{
		{
			olt:    "OLT-SYNTH-A",
			total:  8,
			expect: map[int]int{201: 5, 775: 2, 238: 1},
		},
		{
			olt:    "OLT-SYNTH-B",
			total:  8,
			expect: map[int]int{205: 5, 777: 2, 800: 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.olt, func(t *testing.T) {
			records := ParseZTERunningConfig(goldenFile(t, tc.olt, "running_config.txt"), tc.olt)
			if len(records) != tc.total {
				t.Fatalf("expected %d interface ONU, got %d", tc.total, len(records))
			}

			vlanCounts := map[int]int{}
			for _, rec := range records {
				for _, vlan := range rec.ServiceVLANs {
					vlanCounts[vlan]++
				}
			}

			for vlan, want := range tc.expect {
				if vlanCounts[vlan] != want {
					t.Errorf("VLAN %d = %d, want %d", vlan, vlanCounts[vlan], want)
				}
			}
		})
	}
}

// TestGoldenSystemGroup memvalidasi identitas sistem dari `show system-group`.
func TestGoldenSystemGroup(t *testing.T) {
	cases := []struct {
		olt  string
		name string
		loc  string
	}{
		{olt: "OLT-SYNTH-A", name: "OLT-SYNTH-A", loc: "Synthetic Test Site A"},
		{olt: "OLT-SYNTH-B", name: "OLT-SYNTH-B", loc: "Synthetic Test Site B"},
	}

	for _, tc := range cases {
		t.Run(tc.olt, func(t *testing.T) {
			info := ParseZTESystemGroup(goldenFile(t, tc.olt, "system_group.txt"))

			if info.Model != "C320" {
				t.Errorf("Model = %q, want C320", info.Model)
			}
			if info.Firmware != "V2.1.0" {
				t.Errorf("Firmware = %q, want V2.1.0", info.Firmware)
			}
			if info.SystemName != tc.name {
				t.Errorf("SystemName = %q, want %q", info.SystemName, tc.name)
			}
			if info.Location != tc.loc {
				t.Errorf("Location = %q, want %q", info.Location, tc.loc)
			}
			if info.Uptime == "" {
				t.Error("Uptime kosong")
			}
		})
	}
}

// TestGoldenCard memvalidasi inventaris kartu.
//
// Topologi kartu sengaja dibedakan antar fixture:
//   - OLT-SYNTH-A : 4 kartu, 2 INSERVICE (2 slot OFFLINE)
//   - OLT-SYNTH-B : 4 kartu, 3 INSERVICE + 1 STANDBY
func TestGoldenCard(t *testing.T) {
	cases := []struct {
		olt       string
		inService int
	}{
		{olt: "OLT-SYNTH-A", inService: 2},
		{olt: "OLT-SYNTH-B", inService: 3},
	}

	for _, tc := range cases {
		t.Run(tc.olt, func(t *testing.T) {
			cards := ParseZTECard(goldenFile(t, tc.olt, "card.txt"))
			if len(cards) != 4 {
				t.Fatalf("expected 4 kartu, got %d", len(cards))
			}
			inService := 0
			for _, card := range cards {
				if card.Status == "INSERVICE" {
					inService++
				}
				if card.CfgType == "" {
					t.Errorf("kartu tanpa CfgType: %+v", card)
				}
			}
			if inService != tc.inService {
				t.Errorf("expected %d kartu INSERVICE, got %d", tc.inService, inService)
			}
		})
	}
}

// TestGoldenCardTemperature memvalidasi parsing suhu.
//
// Suhu sensor bergerak pada perangkat nyata, jadi pada fixture sintetis
// nilainya tetap di-assert dalam rentang wajar (20..80 C).
func TestGoldenCardTemperature(t *testing.T) {
	cases := []struct {
		olt   string
		cards int
	}{
		{olt: "OLT-SYNTH-A", cards: 2},
		{olt: "OLT-SYNTH-B", cards: 4},
	}

	for _, tc := range cases {
		t.Run(tc.olt, func(t *testing.T) {
			temps := ParseZTECardTemperature(goldenFile(t, tc.olt, "card_temperature.txt"))
			if len(temps) != tc.cards {
				t.Fatalf("expected %d kartu bersuhu, got %d (%v)", tc.cards, len(temps), temps)
			}
			for slot, temp := range temps {
				if temp < 20 || temp > 80 {
					t.Errorf("suhu slot %s = %v, di luar rentang wajar 20..80 C", slot, temp)
				}
			}
		})
	}
}

// TestGoldenOpticalBulk memvalidasi parser redaman optik BULK per port PON.
//
// Fixture optical menyimpan satu berkas per (port, arah) di subfolder
// `optical/`.
func TestGoldenOpticalBulk(t *testing.T) {
	cases := []struct {
		olt      string
		port     string
		expected int // jumlah ONU pada port tersebut
	}{
		{olt: "OLT-SYNTH-A", port: "1/2/1", expected: 5},
		{olt: "OLT-SYNTH-B", port: "1/1/1", expected: 5},
	}

	for _, tc := range cases {
		t.Run(tc.olt+"/"+tc.port, func(t *testing.T) {
			dir := goldenDirs[tc.olt]
			path := findOpticalGolden(t, dir, tc.port, "olt_rx")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("gagal membaca %s: %v", path, err)
			}

			readings := ParseZTEOpticalPowerBulk(string(raw), "olt-rx")
			if len(readings) != tc.expected {
				t.Fatalf("expected %d entri optik, got %d", tc.expected, len(readings))
			}

			withSignal := 0
			for onuID, reading := range readings {
				if reading.OnuID != onuID {
					t.Errorf("kunci tidak konsisten: %q vs %q", reading.OnuID, onuID)
				}
				if reading.HasSignal {
					withSignal++
					if reading.DBm == nil {
						t.Errorf("ONU %s HasSignal=true tapi DBm nil", onuID)
					}
					if *reading.DBm > 0 {
						t.Errorf("ONU %s punya Rx positif (%.3f dBm) yang tidak wajar", onuID, *reading.DBm)
					}
				} else if reading.Raw == "" {
					t.Errorf("ONU %s tanpa signal tapi juga tanpa nilai mentah", onuID)
				}
			}
			if withSignal == 0 {
				t.Error("tidak ada ONU dengan sinyal optik - parser kemungkinan salah")
			}
		})
	}
}

// findOpticalGolden mencari berkas golden optik untuk (port, arah) tertentu
// di subfolder `optical/`. Pencarian dilakukan berdasarkan pola isi nama file
// agar test tidak rapuh terhadap perubahan konvensi penamaan di cmd/fd_golden.
//
// Fixture bersifat wajib: berkas yang tidak ditemukan adalah kegagalan test,
// bukan kondisi yang boleh dilewati.
func findOpticalGolden(t *testing.T, dir, port, direction string) string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(dir, "optical"))
	if err != nil {
		t.Fatalf("golden optical dump tidak tersedia: %v", err)
	}

	portSlug := strings.NewReplacer("/", "_", "-", "_", ".", "_").Replace(port)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.Contains(name, direction) && strings.Contains(name, portSlug) {
			return filepath.Join(dir, "optical", entry.Name())
		}
	}

	t.Fatalf("golden optik untuk port %s (%s) tidak ditemukan", port, direction)
	return ""
}

// TestGoldenPONPorts memvalidasi ekstraksi port PON aktif dari tabel state.
func TestGoldenPONPorts(t *testing.T) {
	cases := []struct {
		olt    string
		active int
	}{
		{olt: "OLT-SYNTH-A", active: 2},
		{olt: "OLT-SYNTH-B", active: 2},
	}

	for _, tc := range cases {
		t.Run(tc.olt, func(t *testing.T) {
			ports := ParseZTEPONPorts(goldenFile(t, tc.olt, "gpon_onu_state.txt"))
			if len(ports) != tc.active {
				t.Fatalf("expected %d port PON aktif, got %d (%v)", tc.active, len(ports), ports)
			}
			for _, p := range ports {
				if len(p) < 5 {
					t.Errorf("format port tidak wajar: %q", p)
				}
			}
		})
	}
}
