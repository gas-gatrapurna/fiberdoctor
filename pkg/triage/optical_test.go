package triage

import "testing"

// ptr (helper pembungkus *float64) sudah didefinisikan di triage_test.go.

// TestEvaluateOpticalPowerSOPBoundary mengunci konformitas terhadap band SOP
// referensi, termasuk nilai batas yang pernah salah klasifikasi.
func TestEvaluateOpticalPowerSOPBoundary(t *testing.T) {
	cases := []struct {
		dbm  float64
		want OpticalStatus
	}{
		{-15.0, OpticTooHot},
		{-17.999, OpticTooHot},
		{-18.00, OpticNormal},
		{-18.001, OpticNormal},
		{-24.00, OpticNormal},
		{-24.01, OpticWarning},
		{-26.99, OpticWarning},
		{-27.00, OpticCritical}, // <= -27.00 = KRITIS (bukan warning)
		{-27.01, OpticCritical},
		{-35.0, OpticCritical},
	}

	for _, tc := range cases {
		got := EvaluateOpticalPower(&tc.dbm)
		if got != tc.want {
			t.Errorf("EvaluateOpticalPower(%.3f) = %s, want %s", tc.dbm, got, tc.want)
		}
	}

	if got := EvaluateOpticalPower(nil); got != OpticNoSignal {
		t.Errorf("EvaluateOpticalPower(nil) = %s, want NO_LIGHT", got)
	}
}

func TestAnalyzePortOptical(t *testing.T) {
	readings := map[string]*float64{
		"1/2/1:1":  ptr(-20.5),  // NORMAL  (-18 s/d -24)
		"1/2/1:2":  ptr(-15.0),  // TOO_HOT (> -18)
		"1/2/1:3":  ptr(-25.5),  // WARNING (-24.01 s/d -26.99)
		"1/2/1:4":  ptr(-28.2),  // CRITICAL (<= -27)
		"1/2/1:5":  ptr(-30.0),  // CRITICAL
		"1/2/1:6":  nil,         // NO SIGNAL
		"1/2/1:7":  ptr(-22.0),  // NORMAL
		"1/2/1:8":  ptr(-23.9),  // NORMAL (tepat di batas -24)
		"1/2/1:9":  ptr(-26.99), // WARNING (tepat di batas atas warning)
		"1/2/1:10": ptr(-27.0),  // CRITICAL (tepat di batas bawah SOP)
	}

	report := AnalyzePortOptical("1/2/1", readings)

	if report.Total != 10 {
		t.Errorf("Total = %d, want 10", report.Total)
	}
	if report.Normal != 3 {
		t.Errorf("Normal = %d, want 3", report.Normal)
	}
	if report.TooHot != 1 {
		t.Errorf("TooHot = %d, want 1", report.TooHot)
	}
	if report.Warning != 2 {
		t.Errorf("Warning = %d, want 2", report.Warning)
	}
	if report.Critical != 3 {
		t.Errorf("Critical = %d, want 3", report.Critical)
	}
	if report.NoSignal != 1 {
		t.Errorf("NoSignal = %d, want 1", report.NoSignal)
	}
	if report.WorstRxDBm == nil || *report.WorstRxDBm != -30.0 {
		t.Errorf("WorstRxDBm = %v, want -30.0", report.WorstRxDBm)
	}
	if report.WorstOnuID != "1/2/1:5" {
		t.Errorf("WorstOnuID = %q, want 1/2/1:5", report.WorstOnuID)
	}
	if len(report.CriticalOnus) != 3 {
		t.Errorf("CriticalOnus = %v, want 3 entri", report.CriticalOnus)
	}
	// daftar harus terurut agar output deterministik
	if report.CriticalOnus[0] != "1/2/1:4" || report.CriticalOnus[2] != "1/2/1:10" {
		t.Errorf("CriticalOnus tidak terurut: %v", report.CriticalOnus)
	}
}

func TestDiagnosePortOpticalFiberCut(t *testing.T) {
	// Seluruh ONU LOS -> fiber feeder putus.
	readings := map[string]*float64{"1/2/1:1": nil, "1/2/1:2": nil, "1/2/1:3": nil}
	report := AnalyzePortOptical("1/2/1", readings)

	diag := DiagnosePortOptical("OLT-SYNTH-A", "1/2/1", 3, 3, 0, report)
	if diag.DiagnosisCode != "FIBER_CUT_ABSOLUTE" {
		t.Errorf("kode = %q, want FIBER_CUT_ABSOLUTE", diag.DiagnosisCode)
	}
	if diag.Severity != SeverityCritical {
		t.Errorf("severity = %q, want CRITICAL", diag.Severity)
	}
}

func TestDiagnosePortOpticalSingleCritical(t *testing.T) {
	// 1 ONU redaman kritis -> dropcore, level WARNING.
	readings := map[string]*float64{"1/2/1:1": ptr(-28.0), "1/2/1:2": ptr(-21.0)}
	report := AnalyzePortOptical("1/2/1", readings)

	diag := DiagnosePortOptical("OLT-SYNTH-A", "1/2/1", 2, 0, 0, report)
	if diag.DiagnosisCode != "GPON_DROPCORE_FAULT" {
		t.Errorf("kode = %q, want GPON_DROPCORE_FAULT", diag.DiagnosisCode)
	}
	if diag.Severity != SeverityWarning {
		t.Errorf("severity = %q, want WARNING", diag.Severity)
	}
	if diag.AffectedCount != 1 {
		t.Errorf("AffectedCount = %d, want 1", diag.AffectedCount)
	}
}

func TestDiagnosePortOpticalODCDegradation(t *testing.T) {
	// Beberapa ONU redaman kritis -> indikasi cluster ODC, level CRITICAL.
	readings := map[string]*float64{
		"1/2/1:1": ptr(-28.0),
		"1/2/1:2": ptr(-29.5),
		"1/2/1:3": ptr(-21.0),
	}
	report := AnalyzePortOptical("1/2/1", readings)

	diag := DiagnosePortOptical("OLT-SYNTH-A", "1/2/1", 3, 0, 0, report)
	if diag.DiagnosisCode != "GPON_ODC_DEGRADATION" {
		t.Errorf("kode = %q, want GPON_ODC_DEGRADATION", diag.DiagnosisCode)
	}
	if diag.Severity != SeverityCritical {
		t.Errorf("severity = %q, want CRITICAL", diag.Severity)
	}
}

func TestDiagnosePortOpticalEarlyWarning(t *testing.T) {
	// Hanya redaman warning -> flag lebih awal, belum kritis.
	readings := map[string]*float64{"1/2/1:1": ptr(-25.0), "1/2/1:2": ptr(-21.0)}
	report := AnalyzePortOptical("1/2/1", readings)

	diag := DiagnosePortOptical("OLT-SYNTH-A", "1/2/1", 2, 0, 0, report)
	if diag.DiagnosisCode != "GPON_ODC_DEGRADATION" {
		t.Errorf("kode = %q, want GPON_ODC_DEGRADATION", diag.DiagnosisCode)
	}
	if diag.Severity != SeverityWarning {
		t.Errorf("severity = %q, want WARNING", diag.Severity)
	}
}

func TestDiagnosePortOpticalTooHot(t *testing.T) {
	// Redaman terlalu kuat (> -18 dBm) -> flag khusus.
	readings := map[string]*float64{"1/2/1:1": ptr(-12.0), "1/2/1:2": ptr(-13.5)}
	report := AnalyzePortOptical("1/2/1", readings)

	diag := DiagnosePortOptical("OLT-SYNTH-A", "1/2/1", 2, 0, 0, report)
	if diag.DiagnosisCode != "GPON_OPTICAL_TOO_HOT" {
		t.Errorf("kode = %q, want GPON_OPTICAL_TOO_HOT", diag.DiagnosisCode)
	}
	if diag.Severity != SeverityWarning {
		t.Errorf("severity = %q, want WARNING", diag.Severity)
	}
}

func TestDiagnosePortOpticalNormal(t *testing.T) {
	readings := map[string]*float64{"1/2/1:1": ptr(-21.0), "1/2/1:2": ptr(-22.0)}
	report := AnalyzePortOptical("1/2/1", readings)

	diag := DiagnosePortOptical("OLT-SYNTH-A", "1/2/1", 2, 0, 0, report)
	if diag.DiagnosisCode != "GPON_NORMAL" {
		t.Errorf("kode = %q, want GPON_NORMAL", diag.DiagnosisCode)
	}
	if diag.Severity != SeverityInfo {
		t.Errorf("severity = %q, want INFO", diag.Severity)
	}
}
