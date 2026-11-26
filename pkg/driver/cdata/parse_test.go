package cdata

import "testing"

// Fixture di bawah diambil dari manual CLI resmi FD16xx v3.0
// dan diverifikasi terhadap format yang dipakai adapter produksi.

// TestParseONTInfoSummary memvalidasi parser `show ont info <port> all`.
func TestParseONTInfoSummary(t *testing.T) {
	raw := `--------------------------------------------------
F/SP  Port  ONT-ID  SN            Control  Run     Config  Match
                              flag     state   state   state
--------------------------------------------------
0/0    2     3      XSYNT00000001  Active  Online  success match
0/0    2     4      XSYNT00000002  Active  Online  success match
0/0    2     5      XSYNT00000003  Deactive Offline failed  initial
--------------------------------------------------
Total: 3, online: 2, deactive: 0, success: 2, failed: 1
`

	out := ParseONTInfoSummary(raw)
	if len(out.ONTs) != 3 {
		t.Fatalf("expected 3 ONT, got %d", len(out.ONTs))
	}

	first := out.ONTs[0]
	if first.Frame != 0 || first.Slot != 0 || first.Port != 2 || first.ONTID != 3 {
		t.Errorf("koordinat ONT salah: %+v", first)
	}
	if first.SN != "XSYNT00000001" {
		t.Errorf("SN = %q, want XSYNT00000001", first.SN)
	}
	if first.ControlFlag != "active" {
		t.Errorf("ControlFlag = %q, want active", first.ControlFlag)
	}
	if first.RunState != "online" {
		t.Errorf("RunState = %q, want online", first.RunState)
	}
	if first.ConfigState != "success" {
		t.Errorf("ConfigState = %q, want success", first.ConfigState)
	}
	if first.MatchState != "match" {
		t.Errorf("MatchState = %q, want match", first.MatchState)
	}
	if first.Status() != "Online" {
		t.Errorf("Status() = %q, want Online", first.Status())
	}

	third := out.ONTs[2]
	if third.ControlFlag != "deactive" || third.RunState != "offline" {
		t.Errorf("ONT ke-3 salah: %+v", third)
	}
	if third.Status() != "Offline" {
		t.Errorf("Status() ONT ke-3 = %q, want Offline", third.Status())
	}

	if out.Totals == nil {
		t.Fatal("baris Total tidak terparse")
	}
	if out.Totals.Total != 3 || out.Totals.Online != 2 || out.Totals.Failed != 1 || out.Totals.Success != 2 {
		t.Errorf("totals salah: %+v", out.Totals)
	}
}

// TestParseONTInfoDetail memvalidasi parser `show ont info <port> <ont>`.
func TestParseONTInfoDetail(t *testing.T) {
	raw := `--------------------------------------------------
F/S : 0/0
Port : 2
ONT-ID : 3
Control flag : active
Run state : online
Config state : success
Match state : match
DBA type: SR
Distance(m) : 1
Validity mode : always
Authentic mode : sn-auth
SN : XSYNT00000001
Description : Pelanggan demo-subscriber
Last up time : 2000-01-01 09:16:46
Last down time :
--------------------------------------------------
Line Profile-ID : 6
Line Profile-name : 1530
--------------------------------------------------
Service Profile-ID : 6
Service Profile-name : 12
--------------------------------------------------
FEC upstream : Disable
OMCC encrypt : Off
Qos mode : PQ
Mapping mode : VLAN
`

	out := ParseONTInfoDetail(raw)

	if out.SN != "XSYNT00000001" {
		t.Errorf("SN = %q", out.SN)
	}
	if out.Frame != 0 || out.Slot != 0 || out.Port != 2 || out.ONTID != 3 {
		t.Errorf("koordinat salah: %+v", out)
	}
	if out.ControlFlag != "active" || out.RunState != "online" ||
		out.ConfigState != "success" || out.MatchState != "match" {
		t.Errorf("state salah: %+v", out)
	}
	if out.DBAType != "SR" {
		t.Errorf("DBAType = %q, want SR", out.DBAType)
	}
	if out.DistanceM != 1 {
		t.Errorf("DistanceM = %d, want 1", out.DistanceM)
	}
	if out.AuthenticMode != "sn-auth" {
		t.Errorf("AuthenticMode = %q, want sn-auth", out.AuthenticMode)
	}
	if out.Description != "Pelanggan demo-subscriber" {
		t.Errorf("Description = %q", out.Description)
	}
	if out.LineProfileID != 6 || out.LineProfileName != "1530" {
		t.Errorf("line profile salah: %d / %q", out.LineProfileID, out.LineProfileName)
	}
	if out.ServiceProfile != "12" {
		t.Errorf("ServiceProfile = %q, want 12", out.ServiceProfile)
	}
	if out.QoSMode != "PQ" || out.MappingMode != "VLAN" {
		t.Errorf("qos/mapping salah: %q / %q", out.QoSMode, out.MappingMode)
	}
}

// TestParseONTOpticalInfo memvalidasi parser `show ont optical-info`.
func TestParseONTOpticalInfo(t *testing.T) {
	raw := `--------------------------------------------------
Frame/Slot : 0/0
Port : 2
ONT-ID : 3
Voltage(V) : 3.276
Tx optical power(dBm) : 2.310
Rx optical power(dBm) : -21.350
Laser bias current(mA) : 33.000
Temperature(C) : 41.000
OLT Rx ONT optical power(dBm) : -21.350
--------------------------------------------------
`

	out := ParseONTOpticalInfo(raw)

	if out.Port != 2 || out.ONTID != 3 {
		t.Errorf("koordinat salah: %+v", out)
	}
	if out.Voltage == nil || *out.Voltage != 3.276 {
		t.Errorf("Voltage = %v", out.Voltage)
	}
	if out.TxPower == nil || *out.TxPower != 2.310 {
		t.Errorf("TxPower = %v", out.TxPower)
	}
	if out.RxPower == nil || *out.RxPower != -21.350 {
		t.Errorf("RxPower = %v", out.RxPower)
	}
	if out.BiasCurrent == nil || *out.BiasCurrent != 33.000 {
		t.Errorf("BiasCurrent = %v", out.BiasCurrent)
	}
	if out.Temperature == nil || *out.Temperature != 41.000 {
		t.Errorf("Temperature = %v", out.Temperature)
	}
	if out.OLTRxOntPower == nil || *out.OLTRxOntPower != -21.350 {
		t.Errorf("OLTRxOntPower = %v", out.OLTRxOntPower)
	}
}

// TestParseRegisterStatistics memvalidasi parser `show ont register-statistics all`.
func TestParseRegisterStatistics(t *testing.T) {
	raw := `--------------------------------------------------
F/SP  Port  AutoFind  Authenticated  Online
--------------------------------------------------
0/0    1     3         3             3
0/0    2     5         4             4
--------------------------------------------------
`

	stats := ParseRegisterStatistics(raw)
	if len(stats) != 2 {
		t.Fatalf("expected 2 baris, got %d", len(stats))
	}
	if stats[0].Port != 1 || stats[0].AutoFind != 3 || stats[0].Authenticated != 3 || stats[0].Online != 3 {
		t.Errorf("baris 1 salah: %+v", stats[0])
	}
	if stats[1].Port != 2 || stats[1].AutoFind != 5 || stats[1].Authenticated != 4 || stats[1].Online != 4 {
		t.Errorf("baris 2 salah: %+v", stats[1])
	}
}

// TestParseServiceStatus memvalidasi parser `show service status`.
func TestParseServiceStatus(t *testing.T) {
	raw := `--------------------------------------------------
Service-name   Enable-status
--------------------------------------------------
telnet         enable
ssh            disable
http           enable
https          enable
snmp           enable
`

	out := ParseServiceStatus(raw)
	if out.Services["telnet"] != "enable" {
		t.Errorf("telnet = %q, want enable", out.Services["telnet"])
	}
	if out.Services["ssh"] != "disable" {
		t.Errorf("ssh = %q, want disable", out.Services["ssh"])
	}
	if out.Services["snmp"] != "enable" {
		t.Errorf("snmp = %q, want enable", out.Services["snmp"])
	}
}

// TestResolveModel memvalidasi pengenalan varian C-Data.
func TestResolveModel(t *testing.T) {
	cases := []struct {
		in        string
		wantModel string
		wantPorts int
	}{
		{"FD1602S-B1", "FD1602S-B1", 2},
		{"fd1602s-b1", "FD1602S-B1", 2},
		{"FD1602S_B1_Pack_IW_V3.1.22_241031_3920_X000.img", "FD1602S-B1", 2},
		{"FD1608S-B1", "FD1608S-B1", 8},
		{"FD1616S-B1", "FD1616S-B1", 16},
		{"FD1601S-B1", "FD1601S-B1", 1},
		{"FD804G(W)(D)", "FD804G(W)(D)", 0},
		{"FD602XW", "FD602XW", 0},
	}

	for _, tc := range cases {
		profile, ok := ResolveModel(tc.in)
		if !ok {
			t.Errorf("ResolveModel(%q) gagal mengenali model", tc.in)
			continue
		}
		if profile.Model != tc.wantModel {
			t.Errorf("ResolveModel(%q).Model = %q, want %q", tc.in, profile.Model, tc.wantModel)
		}
		if profile.GPONPorts != tc.wantPorts {
			t.Errorf("ResolveModel(%q).GPONPorts = %d, want %d", tc.in, profile.GPONPorts, tc.wantPorts)
		}
		if profile.Vendor != "cdata" {
			t.Errorf("ResolveModel(%q).Vendor = %q", tc.in, profile.Vendor)
		}
	}

	if _, ok := ResolveModel("Huawei MA5608T"); ok {
		t.Error("ResolveModel seharusnya menolak model non-C-Data")
	}
}

// TestKnownModelsCoversFleet memastikan varian yang dipakai di lapangan
// (terlihat dari backup konfigurasi perangkat) terdaftar.
func TestKnownModelsCoversFleet(t *testing.T) {
	known := map[string]bool{}
	for _, m := range KnownModels() {
		known[m] = true
	}

	for _, want := range []string{
		"FD1601S-B1", "FD1602S-B1", "FD1604S-B1", "FD1608S-B1", "FD1612S-B1", "FD1616S-B1",
		"FD12xx", "FD11xx", "FD602XW", "FD704G(W)(D)", "FD804G(W)(D)",
	} {
		if !known[want] {
			t.Errorf("varian %q belum terdaftar di KnownModels()", want)
		}
	}
}
