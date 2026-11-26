package triage

import (
	"testing"
)

func TestEvaluateOpticalPower(t *testing.T) {
	cases := []struct {
		val      *float64
		expected OpticalStatus
	}{
		{nil, OpticNoSignal},
		{ptr(-17.5), OpticTooHot},
		{ptr(-20.0), OpticNormal},
		{ptr(-25.5), OpticWarning},
		{ptr(-28.0), OpticCritical},
	}

	for _, c := range cases {
		got := EvaluateOpticalPower(c.val)
		if got != c.expected {
			t.Errorf("for val %v expected %s, got %s", c.val, c.expected, got)
		}
	}
}

func TestDiagnoseGPONTriage(t *testing.T) {
	// Case 1: Dropcore fault (1 ONU bad)
	d1 := DiagnoseGPONTriage("OLT-SYNTH-A", "1/2/1", 32, 1, 0, -28.5)
	if d1.DiagnosisCode != "GPON_DROPCORE_FAULT" {
		t.Errorf("expected GPON_DROPCORE_FAULT, got %s", d1.DiagnosisCode)
	}

	// Case 2: ODC degradation (multiple ONUs bad)
	d2 := DiagnoseGPONTriage("OLT-SYNTH-A", "1/2/1", 32, 4, 1, -29.0)
	if d2.DiagnosisCode != "GPON_ODC_DEGRADATION" {
		t.Errorf("expected GPON_ODC_DEGRADATION, got %s", d2.DiagnosisCode)
	}

	// Case 3: Absolute fiber cut (all ONUs in LOS)
	d3 := DiagnoseGPONTriage("OLT-SYNTH-A", "1/2/1", 32, 0, 32, -40.0)
	if d3.DiagnosisCode != "FIBER_CUT_ABSOLUTE" {
		t.Errorf("expected FIBER_CUT_ABSOLUTE, got %s", d3.DiagnosisCode)
	}
}

func ptr(v float64) *float64 {
	return &v
}
