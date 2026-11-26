package zte

import (
	"strings"
	"testing"
)

// TestParseZTEONUState memakai format output ASLI perangkat (live capture
// 2026-11-05, OLT-SYNTH-A). Test sebelumnya memvalidasi format
// fiktif (ada kolom "SN" & "OMCI State" yang tidak pernah muncul di perangkat).
func TestParseZTEONUState(t *testing.T) {
	raw := `
OnuIndex   Admin State  OMCC State  Phase State  Channel
--------------------------------------------------------------
1/2/1:1     enable       enable      working      1(GPON)
1/2/3:30    enable       disable     OffLine      1(GPON)
1/2/1:33    enable       disable     DyingGasp    1(GPON)
1/2/15:7    enable       disable     LOS          1(GPON)
`

	records, err := ParseZTEONUState(raw, "OLT-SYNTH-A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(records))
	}

	if records[0].ONUID != "1/2/1:1" || records[0].Status != "Online" {
		t.Errorf("record 0 mismatch: %+v", records[0])
	}
	if records[0].PON.Slot != 1 || records[0].PON.Subslot != 2 || records[0].PON.Port != 1 || records[0].PON.Index != 1 {
		t.Errorf("record 0 PON mismatch: %+v", records[0].PON)
	}
	if records[0].AdminState != "enable" || records[0].OMCCState != "enable" || records[0].PhaseState != "working" {
		t.Errorf("record 0 state columns mismatch: %+v", records[0])
	}
	if records[0].Channel != "1(GPON)" {
		t.Errorf("record 0 channel mismatch: %q", records[0].Channel)
	}

	if records[1].Status != "Offline" {
		t.Errorf("record 1 expected Offline, got %s", records[1].Status)
	}
	if records[2].Status != "DyingGasp" {
		t.Errorf("record 2 expected DyingGasp, got %s", records[2].Status)
	}
	if records[3].Status != "LOS" {
		t.Errorf("record 3 expected LOS, got %s", records[3].Status)
	}
}

func TestMapZTEPhaseState(t *testing.T) {
	cases := map[string]string{
		"working":   "Online",
		"ready":     "Online",
		"LOS":       "LOS",
		"OffLine":   "Offline",
		"DyingGasp": "DyingGasp",
		"unknown":   "Offline",
	}
	for in, want := range cases {
		if got := MapZTEPhaseState(in); got != want {
			t.Errorf("MapZTEPhaseState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseZTESNIndexFromRunningConfig(t *testing.T) {
	raw := `interface gpon-olt_1/2/15
  no shutdown
  description PANGGUNG
  onu 1 type V2801RGW sn ZTEGSYNTH000002
  onu 2 type ALL sn ZTEGSYNTH000003
!
interface gpon-onu_1/2/15:1
  name demo-subscriber-01
!
`

	registry := ParseZTERegistry(raw)
	if got := registry["1/2/15:1"].SN; got != "ZTEGSYNTH000002" {
		t.Errorf("SN 1/2/15:1 = %q, want ZTEGSYNTH000002", got)
	}
	if got := registry["1/2/15:1"].DeviceType; got != "V2801RGW" {
		t.Errorf("DeviceType 1/2/15:1 = %q, want V2801RGW", got)
	}
	if got := registry["1/2/15:2"].SN; got != "ZTEGSYNTH000003" {
		t.Errorf("SN 1/2/15:2 = %q, want ZTEGSYNTH000003", got)
	}

	snIndex := ParseZTESNIndex(raw)
	if len(snIndex) != 2 {
		t.Errorf("expected 2 SN entries, got %d", len(snIndex))
	}
}

func TestParseZTERunningConfig(t *testing.T) {
	raw := `interface gpon-onu_1/2/15:10
  name demo-subscriber-01
  sn-bind enable sn
  tcont 1 profile SYNTH-1G-UP
  gemport 1 tcont 1 queue 1
  service-port 1 vport 1 user-vlan 201 vlan 201
!
interface gpon-onu_1/2/15:11
  name Bambang Tri Geblog
  tcont 1 name SVC-1-VLAN0201 profile SYNTH-75M-UP
  service-port 1 vport 1 user-vlan 201 vlan 201
  service-port 2 vport 2 user-vlan 775 vlan 775
!
`

	records := ParseZTERunningConfig(raw, "OLT-SYNTH-A")
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	first := records[0]
	if first.ONUID != "1/2/15:10" {
		t.Errorf("ONUID = %q", first.ONUID)
	}
	if first.CustomerName != "demo-subscriber-01" {
		t.Errorf("CustomerName = %q", first.CustomerName)
	}
	if first.TContProfile != "SYNTH-1G-UP" {
		t.Errorf("TContProfile = %q", first.TContProfile)
	}
	if len(first.ServiceVLANs) != 1 || first.ServiceVLANs[0] != 201 {
		t.Errorf("ServiceVLANs = %v", first.ServiceVLANs)
	}

	second := records[1]
	if second.TContProfile != "SYNTH-75M-UP" {
		t.Errorf("TContProfile (dengan name) = %q", second.TContProfile)
	}
	if len(second.ServiceVLANs) != 2 {
		t.Errorf("ServiceVLANs = %v, expected 2 entries", second.ServiceVLANs)
	}
}

func TestCleanCLIOutput(t *testing.T) {
	raw := "\r\nshow card\r\nRack Shelf Slot\r\nOLT-SYNTH-A#"
	got := CleanCLIOutput(raw, "show card", "OLT-SYNTH-A#")

	if strings.Contains(got, "show card") {
		t.Errorf("echo perintah belum dibuang: %q", got)
	}
	if strings.Contains(got, "OLT-SYNTH-A#") {
		t.Errorf("prompt belum dibuang: %q", got)
	}
	if !strings.Contains(got, "Rack Shelf Slot") {
		t.Errorf("isi output hilang: %q", got)
	}
}

func TestParseZTEOpticalPower(t *testing.T) {
	raw := `           OLT                  ONU              Attenuation
--------------------------------------------------------------------------
 up      Rx :no signal         Tx:N/A               N/A

 down    Tx :10.496(dbm)       Rx:N/A               N/A
`

	power := ParseZTEOpticalPower(raw)
	if power.OLTRxDBm != nil {
		t.Errorf("OLT Rx harus nil saat no signal, got %v", *power.OLTRxDBm)
	}
	if power.HasSignal {
		t.Error("HasSignal harus false saat no signal")
	}
	if power.OLTTxDBm == nil || *power.OLTTxDBm != 10.496 {
		t.Errorf("OLT Tx = %v, expected 10.496", power.OLTTxDBm)
	}
}
