package zte

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
)

// Family adalah keluarga produk ZTE OLT.
type Family string

const (
	FamilyC300  Family = "C300"
	FamilyC3000 Family = "C3000"
	FamilyC320  Family = "C320"
	FamilyC600  Family = "C600"
)

// ModelProfile menjelaskan kapabilitas sebuah varian ZTE OLT.
//
// Penting: profil TIDAK boleh berisi asumsi yang belum diverifikasi.
// Kolom Verified menandai apakah behaviour sudah dibuktikan terhadap perangkat
// live atau baru berupa penyimpulan dokumentasi.
type ModelProfile struct {
	Vendor      string
	Model       string
	Family      Family
	DisplayName string

	// Verified true bila seluruh behaviour profil ini sudah diuji ke
	// perangkat live.
	Verified bool

	// StateHasSNColumn menandai bahwa tabel `show gpon onu state` pada
	// firmware ini memuat kolom Serial Number.
	//
	// C320 V2.1.0 (SYNTH-A & SYNTH-B, diverifikasi 2026-11-05): TIDAK ADA
	// kolom SN. Karena itu parser mencoba dua tata letak dan memakai yang
	// menghasilkan rekaman.
	StateHasSNColumn bool

	// SupportsRunningConfig menandai `show running-config` tersedia (sumber
	// SN & konfigurasi ONU).
	SupportsRunningConfig bool
	// SupportsBulkOptical menandai `show pon power olt-rx/onu-rx` per port
	// PON tersedia (diverifikasi pada C320 V2.1.0).
	SupportsBulkOptical bool
	// SupportsUnconfiguredONU menandai `show gpon onu uncfg` tersedia.
	SupportsUnconfiguredONU bool
	// SupportsCardTemperature menandai `show card-temperature` tersedia.
	SupportsCardTemperature bool
}

// zteProfiles adalah katalog varian ZTE yang dikenali.
var zteProfiles = map[string]ModelProfile{
	"zte_c320": {
		Vendor: "ZTE", Model: "C320", Family: FamilyC320,
		DisplayName: "ZTE C320",
		// Diverifikasi live terhadap OLT-SYNTH-A & OLT-SYNTH-B
		// (C320 Version V2.1.0) pada 2026-11-05.
		Verified:                true,
		StateHasSNColumn:        false,
		SupportsRunningConfig:   true,
		SupportsBulkOptical:     true,
		SupportsUnconfiguredONU: true,
		SupportsCardTemperature: true,
	},
	"zte_c300": {
		Vendor: "ZTE", Model: "C300", Family: FamilyC300,
		DisplayName: "ZTE C300",
		// BELUM diverifikasi terhadap perangkat live. Profil ini disusun dari
		// kesamaan platform CLI C300/C320; Unterschied nyata akan
		// terdeteksi begitu golden dump C300 diambil.
		Verified:                false,
		StateHasSNColumn:        false,
		SupportsRunningConfig:   true,
		SupportsBulkOptical:     false,
		SupportsUnconfiguredONU: true,
		SupportsCardTemperature: true,
	},
	"zte_c3000": {
		Vendor: "ZTE", Model: "C3000", Family: FamilyC3000,
		DisplayName:             "ZTE C3000",
		Verified:                false,
		StateHasSNColumn:        false,
		SupportsRunningConfig:   true,
		SupportsBulkOptical:     false,
		SupportsUnconfiguredONU: true,
		SupportsCardTemperature: false,
	},
	"zte_c600": {
		Vendor: "ZTE", Model: "C600", Family: FamilyC600,
		DisplayName:             "ZTE C600",
		Verified:                false,
		StateHasSNColumn:        false,
		SupportsRunningConfig:   true,
		SupportsBulkOptical:     false,
		SupportsUnconfiguredONU: true,
		SupportsCardTemperature: false,
	},
}

// ResolveZTEModel mengembalikan profil varian untuk kunci vendor.
func ResolveZTEModel(vendorKey string) (ModelProfile, bool) {
	profile, ok := zteProfiles[strings.ToLower(vendorKey)]
	return profile, ok
}

// KnownZTEModels mengembalikan daftar kunci vendor yang dikenali.
func KnownZTEModels() []string {
	return []string{"zte_c300", "zte_c3000", "zte_c320", "zte_c600"}
}

// reStateLineWithSN adalah tata letak LAMA tabel state, dengan asumsi kolom
// Serial Number ada.
//
// Tata letak ini dipakai sebagai fallback untuk varian/firmware yang memang
// menampilkan SN di tabel state. Parser utama (C320) tidak memakainya.
var reStateLineWithSN = regexp.MustCompile(
	`^(\d+)/(\d+)/(\d+):(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+([A-Za-z0-9]{12,16})`)

// ParseZTEONUStateAuto mem-parsing `show gpon onu state` dengan mendeteksi
// tata letak kolom secara otomatis.
//
// Dua tata letak yang mungkin:
//
//	(1) OnuIndex | Admin State | OMCC State | Phase State | Channel        (C320 V2.1.0, tanpa SN)
//	(2) OnuIndex | Admin State | OMCI State | Phase State | Channel | SN   (tata letak lama)
//
// Fungsi ini mengembalikan rekaman dari tata letak yang menghasilkan data;
// bila keduanya tidak menghasilkan apa pun, hasil dari tata letak utama
// dikembalikan (kosong) beserta errornya.
func ParseZTEONUStateAuto(rawOutput, oltName string) ([]driver.ONURecord, error) {
	primary, err := ParseZTEONUState(rawOutput, oltName)
	if err == nil && len(primary) > 0 {
		return primary, nil
	}

	// Fallback: tata letak dengan kolom SN.
	var legacy []driver.ONURecord
	for _, line := range strings.Split(rawOutput, "\n") {
		line = cleanLine(line)
		m := reStateLineWithSN.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		slot, _ := strconv.Atoi(strings.Split(m[1], "/")[0])
		subslot, _ := strconv.Atoi(strings.Split(m[1], "/")[1])
		port, _ := strconv.Atoi(strings.Split(m[1], "/")[2])
		index, _ := strconv.Atoi(m[2])

		legacy = append(legacy, driver.ONURecord{
			OLT:        oltName,
			ONUID:      m[1] + ":" + m[2],
			AdminState: m[3],
			OMCCState:  m[4],
			PhaseState: m[5],
			Channel:    m[6],
			SN:         m[7],
			Status:     MapZTEPhaseState(m[5]),
			PON: driver.PONLocation{
				Slot:    slot,
				Subslot: subslot,
				Port:    port,
				Index:   index,
			},
			LastUpdated: time.Now(),
		})
	}
	if len(legacy) > 0 {
		return legacy, nil
	}

	return primary, err
}
