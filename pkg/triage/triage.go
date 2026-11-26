package triage

import (
	"fmt"
	"time"
)

// OpticalStatus kategori redaman optik sesuai SOP referensi.
type OpticalStatus string

const (
	OpticNormal   OpticalStatus = "NORMAL"   // -18.00 s/d -24.00 dBm
	OpticTooHot   OpticalStatus = "TOO_HOT"  // > -18.00 dBm
	OpticWarning  OpticalStatus = "WARNING"  // -24.01 s/d -26.99 dBm
	OpticCritical OpticalStatus = "CRITICAL" // <= -27.00 dBm
	OpticNoSignal OpticalStatus = "NO_LIGHT" // LOS / tidak ada sinyal
)

// EvaluateOpticalPower mengevaluasi level redaman optik berdasarkan standar baku referensi.
//
// Banding SOP (MASTER_FIBER_DOCTOR_SSOT.md §3):
//   - dbm >  -18.00            : TOO_HOT   (terlalu dekat splitter)
//   - -18.00 .. -24.00         : NORMAL    (gold standard)
//   - -24.01 .. -26.99         : WARNING   (redaman tinggi / bending / kotor)
//   - dbm <= -27.00            : CRITICAL  (risiko LOS / packet loss)
//
// Perhatikan batas bawah WARNING: -27.00 sudah termasuk CRITICAL, sehingga
// perbandingannya harus `> -27.00` (bukan `>= -27.00`).
func EvaluateOpticalPower(rxDBm *float64) OpticalStatus {
	if rxDBm == nil {
		return OpticNoSignal
	}
	dbm := *rxDBm
	switch {
	case dbm > -18.00:
		return OpticTooHot
	case dbm >= -24.00:
		return OpticNormal
	case dbm > -27.00:
		return OpticWarning
	default:
		return OpticCritical
	}
}

// FaultSeverity tingkat keparahan gangguan.
type FaultSeverity string

const (
	SeverityInfo     FaultSeverity = "INFO"
	SeverityWarning  FaultSeverity = "WARNING"
	SeverityCritical FaultSeverity = "CRITICAL"
)

// IncidentDiagnosis hasil diagnosis gangguan jaringan FTTH.
type IncidentDiagnosis struct {
	IncidentID     string        `json:"incident_id"`
	DiagnosisCode  string        `json:"diagnosis_code"` // GPON_DROPCORE_FAULT, GPON_ODC_DEGRADATION, FIBER_CUT_ABSOLUTE
	OSILayer       string        `json:"osi_layer"`      // L1, L2, L3, L7
	Severity       FaultSeverity `json:"severity"`
	AffectedPort   string        `json:"affected_port"`
	AffectedCount  int           `json:"affected_count"`
	BlastRadius    string        `json:"blast_radius"` // 1 Pelanggan, 1 ODC Cluster, Backbone
	RootCause      string        `json:"root_cause"`
	Recommendation string        `json:"recommendation"`
	Timestamp      time.Time     `json:"timestamp"`
}

// DiagnoseGPONTriage mendiagnosis gangguan pada port PON berdasarkan pola modem yang bermasalah.
func DiagnoseGPONTriage(oltName, ponPort string, totalONUs, criticalCount, losCount int, worstRx float64) IncidentDiagnosis {
	ts := time.Now()
	incID := fmt.Sprintf("INC-%s-%d", oltName, ts.Unix())

	// Kasus 1: Putus Fisik Total (Fiber Cut pada feeder OLT -> ODC)
	if totalONUs > 0 && losCount >= totalONUs {
		return IncidentDiagnosis{
			IncidentID:     incID,
			DiagnosisCode:  "FIBER_CUT_ABSOLUTE",
			OSILayer:       "L1 (Physical)",
			Severity:       SeverityCritical,
			AffectedPort:   ponPort,
			AffectedCount:  losCount,
			BlastRadius:    "Seluruh Port PON (Kabel Feeder ODC Putus)",
			RootCause:      "Kabel distribusi utama putus total (Loss of Signal massal)",
			Recommendation: "Kirim tim maintenance FO untuk tracing OTDR dari OLT ke ODC pertama",
			Timestamp:      ts,
		}
	}

	// Kasus 2: Degradasi ODC (Beberapa modem sekaligus redaman buruk / LOS di cluster yang sama)
	if (criticalCount + losCount) > 1 {
		return IncidentDiagnosis{
			IncidentID:     incID,
			DiagnosisCode:  "GPON_ODC_DEGRADATION",
			OSILayer:       "L1 (Physical)",
			Severity:       SeverityCritical,
			AffectedPort:   ponPort,
			AffectedCount:  criticalCount + losCount,
			BlastRadius:    "1 Cluster ODC / Splitter Box",
			RootCause:      fmt.Sprintf("Distribusi redaman splitter drop drastis (worst Rx: %.2f dBm)", worstRx),
			Recommendation: "Cek konektor pigtail dan redaman input ODC di lapangan",
			Timestamp:      ts,
		}
	}

	// Kasus 3: Gangguan Dropcore Spesifik 1 Pelanggan
	if (criticalCount + losCount) == 1 {
		return IncidentDiagnosis{
			IncidentID:     incID,
			DiagnosisCode:  "GPON_DROPCORE_FAULT",
			OSILayer:       "L1 (Physical)",
			Severity:       SeverityWarning,
			AffectedPort:   ponPort,
			AffectedCount:  1,
			BlastRadius:    "1 Pelanggan Spesifik (Last-Mile)",
			RootCause:      fmt.Sprintf("Kabel dropcore rumah pelanggan terjepit atau konektor kotor (Rx: %.2f dBm)", worstRx),
			Recommendation: "Kirim teknisi lokal cek kabel dropcore dari ODP ke roset modem pelanggan",
			Timestamp:      ts,
		}
	}

	// Kasus Normal
	return IncidentDiagnosis{
		IncidentID:     incID,
		DiagnosisCode:  "GPON_NORMAL",
		OSILayer:       "L1 (Physical)",
		Severity:       SeverityInfo,
		AffectedPort:   ponPort,
		AffectedCount:  0,
		BlastRadius:    "Normal",
		RootCause:      "Redaman dan status registrasi seluruh ONU dalam batas toleransi",
		Recommendation: "Tidak ada tindakan yang diperlukan",
		Timestamp:      ts,
	}
}
