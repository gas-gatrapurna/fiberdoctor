package triage

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PortOpticalReport adalah hasil analisis redaman optik seluruh ONU pada satu
// port PON.
//
// Sumber data: `show pon power olt-rx gpon-olt_<slot>/<subslot>/<port>` yang
// mengembalikan redaman OLT-Rx untuk SEMUA ONU pada port tersebut dalam satu
// panggilan CLI (bukan satu panggilan per ONU).
type PortOpticalReport struct {
	PONPort      string
	Total        int
	NoSignal     int // ONU tanpa cahaya di OLT (LOS / mati)
	TooHot       int // > -18.00 dBm
	Normal       int // -18.00 s/d -24.00 dBm (gold standard)
	Warning      int // -24.01 s/d -26.99 dBm
	Critical     int // <= -27.00 dBm
	WorstRxDBm   *float64
	WorstOnuID   string
	CriticalOnus []string
	WarningOnus  []string
}

// AnalyzePortOptical menganalisis redaman OLT-Rx seluruh ONU pada satu port.
//
// rxByOnu memetakan ONU ID ("1/2/15:10") ke nilai redaman dBm. Nilai nil
// berarti tidak ada sinyal optik (LOS / ONU mati).
func AnalyzePortOptical(ponPort string, rxByOnu map[string]*float64) PortOpticalReport {
	report := PortOpticalReport{PONPort: ponPort}

	for onuID, rx := range rxByOnu {
		report.Total++

		if rx == nil {
			report.NoSignal++
			continue
		}

		switch EvaluateOpticalPower(rx) {
		case OpticTooHot:
			report.TooHot++
		case OpticNormal:
			report.Normal++
		case OpticWarning:
			report.Warning++
			report.WarningOnus = append(report.WarningOnus, onuID)
		case OpticCritical:
			report.Critical++
			report.CriticalOnus = append(report.CriticalOnus, onuID)
		}

		if report.WorstRxDBm == nil || *rx < *report.WorstRxDBm {
			value := *rx
			report.WorstRxDBm = &value
			report.WorstOnuID = onuID
		}
	}

	sortONUIDs(report.CriticalOnus)
	sortONUIDs(report.WarningOnus)
	return report
}

// sortONUIDs mengurutkan daftar ONU ID ("1/2/15:10") secara NUMERIK per
// komponen (slot/subslot/port:index), bukan lexicografis.
//
// _lexicografis_: "1/2/1:10" akan mendahului "1/2/1:4" karena '1' < '4'.
// Urutan numerik jauh lebih mudah dibaca di laporan NOC.
func sortONUIDs(ids []string) {
	sort.Slice(ids, func(i, j int) bool {
		return compareONUID(ids[i], ids[j]) < 0
	})
}

// compareONUID membandingkan dua ONU ID secara numerik per komponen.
func compareONUID(a, b string) int {
	pa := parseONUID(a)
	pb := parseONUID(b)
	for k := 0; k < len(pa) && k < len(pb); k++ {
		if pa[k] != pb[k] {
			if pa[k] < pb[k] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	return 0
}

// parseONUID mengubah "1/2/15:10" menjadi [1, 2, 15, 10].
func parseONUID(id string) []int {
	replaced := strings.NewReplacer("/", " ", ":", " ").Replace(id)
	fields := strings.Fields(replaced)
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// DiagnosePortOptical adalah diagnosis gangguan port PON yang diperkaya bukti
// redaman optik per ONU.
//
// Parameter:
//   - totalONUs   : jumlah ONU pada port
//   - losCount    : ONU berstatus LOS
//   - dyingGasp   : ONU berstatus DyingGasp
//   - report      : hasil AnalyzePortOptical
func DiagnosePortOptical(oltName, ponPort string, totalONUs, losCount, dyingGasp int, report PortOpticalReport) IncidentDiagnosis {
	worstRx := 0.0
	if report.WorstRxDBm != nil {
		worstRx = *report.WorstRxDBm
	}
	registrationCritical := losCount + dyingGasp

	// Kasus 1: seluruh ONU pada port kehilangan sinyal -> fiber feeder putus.
	if totalONUs > 0 && losCount >= totalONUs {
		return DiagnoseGPONTriage(oltName, ponPort, totalONUs, registrationCritical, losCount, worstRx)
	}

	// Kasus 2: ada ONU dengan redaman kritis (>= -27 dBm).
	if report.Critical > 0 {
		severity := SeverityWarning
		code := "GPON_DROPCORE_FAULT"
		blast := "1 Pelanggan Spesifik (Last-Mile)"
		recommendation := fmt.Sprintf(
			"Kirim teknisi lokal cek kabel dropcore ODP-roset dan kebersihan konektor ONU %s (Rx %.2f dBm)",
			firstOr(report.CriticalOnus, "-"), worstRx)

		if report.Critical > 1 || registrationCritical > 1 {
			severity = SeverityCritical
			code = "GPON_ODC_DEGRADATION"
			blast = "1 Cluster ODC / Splitter Box"
			recommendation = fmt.Sprintf(
				"Cek konektor pigtail & redaman input ODC (worst Rx %.2f dBm pada %s); %.0f ONU dalam status kritis",
				worstRx, firstOr(report.CriticalOnus, "-"), float64(report.Critical))
		}

		return IncidentDiagnosis{
			IncidentID:     fmt.Sprintf("INC-%s-%s-%d", oltName, ponPort, report.Critical),
			DiagnosisCode:  code,
			OSILayer:       "L1 (Physical)",
			Severity:       severity,
			AffectedPort:   ponPort,
			AffectedCount:  report.Critical,
			BlastRadius:    blast,
			RootCause:      fmt.Sprintf("Redaman optik kritis pada %d ONU (worst Rx: %.2f dBm)", report.Critical, worstRx),
			Recommendation: recommendation,
			Timestamp:      time.Now(),
		}
	}

	// Kasus 3: lebih dari satu ONU bermasalah pada level registrasi.
	if registrationCritical > 1 {
		return DiagnoseGPONTriage(oltName, ponPort, totalONUs, registrationCritical, losCount, worstRx)
	}

	// Kasus 4: tepat satu ONU bermasalah.
	if registrationCritical == 1 {
		return DiagnoseGPONTriage(oltName, ponPort, totalONUs, registrationCritical, losCount, worstRx)
	}

	// Kasus 5: redaman warning (belum kritis, tapi tren memburuk) -> flag lebih awal.
	if report.Warning > 0 {
		return IncidentDiagnosis{
			IncidentID:     fmt.Sprintf("INC-%s-%s-%d", oltName, ponPort, report.Warning),
			DiagnosisCode:  "GPON_ODC_DEGRADATION",
			OSILayer:       "L1 (Physical)",
			Severity:       SeverityWarning,
			AffectedPort:   ponPort,
			AffectedCount:  report.Warning,
			BlastRadius:    "Indikasi Awal 1 Cluster ODC / Splitter Box",
			RootCause:      fmt.Sprintf("%d ONU pada redaman warning (worst Rx: %.2f dBm)", report.Warning, worstRx),
			Recommendation: "Jadwalkan pemeriksaan preventif konektor ODC & bending kabel sebelum naik ke status kritis",
			Timestamp:      time.Now(),
		}
	}

	// Kasus 6: seluruh ONU dalam rentang aman.
	diag := DiagnoseGPONTriage(oltName, ponPort, totalONUs, registrationCritical, losCount, worstRx)
	if report.TooHot > 0 {
		diag.Severity = SeverityWarning
		diag.DiagnosisCode = "GPON_OPTICAL_TOO_HOT"
		diag.AffectedCount = report.TooHot
		diag.RootCause = fmt.Sprintf("%d ONU redaman terlalu kuat (> -18 dBm): terlalu dekat splitter", report.TooHot)
		diag.Recommendation = "Evaluasi jumlah splitter seri pada ODC sesuai desain jaringan"
	}
	return diag
}

func firstOr(list []string, fallback string) string {
	if len(list) == 0 {
		return fallback
	}
	return list[0]
}
