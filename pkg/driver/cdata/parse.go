// Package cdata mengimplementasikan driver OLT untuk perangkat C-Data
// (Shenzhen C-Data Technology) seri FD.
//
// Ground truth:
//   - Manual CLI resmi FD16xx v3.0 (2022-09-01)
//   - Adapter Python referensi yang berjalan di produksi (tidak dipublikasikan)
//
// Perbedaan penting dari ZTE C320:
//   - C-Data memakai ONT (bukan ONU) sebagai terminologi.
//   - CLI C-Data memakai "interface view" per PON: `config-interface-gpon-0/0`,
//     dan perintah dijalankan dari config view ATAU gpon interface view.
//   - Perintah inventaris adalah `show ont info <port-id> all` (bukan
//     `show gpon onu state`), dan tabelnya memuat kolom SN.
package cdata

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ONTInfo merepresentasikan registrasi & status satu ONT (Optical Network
// Terminal) pada OLT C-Data.
type ONTInfo struct {
	Frame int `json:"frame"`
	Slot  int `json:"slot"`
	Port  int `json:"port"`
	ONTID int `json:"ont_id"`

	SN           string `json:"sn"`
	ControlFlag  string `json:"control_flag"` // active / deactive
	RunState     string `json:"run_state"`    // online / offline
	ConfigState  string `json:"config_state"` // initial / failed / success
	MatchState   string `json:"match_state"`  // initial / match / mismatch
	LastDownTime string `json:"last_down_time,omitempty"`
	Description  string `json:"description,omitempty"`

	// Detail (hanya terisi dari `show ont info <port> <ont-id>`).
	DBAType         string `json:"dba_type,omitempty"`
	DistanceM       int    `json:"distance_m,omitempty"`
	ValidityMode    string `json:"validity_mode,omitempty"`
	AuthenticMode   string `json:"authentic_mode,omitempty"`
	LineProfileID   int    `json:"line_profile_id,omitempty"`
	LineProfileName string `json:"line_profile_name,omitempty"`
	ServiceProfile  string `json:"service_profile_name,omitempty"`
	FECUpstream     string `json:"fec_upstream,omitempty"`
	OMCCEncrypt     string `json:"omcc_encrypt,omitempty"`
	QoSMode         string `json:"qos_mode,omitempty"`
	MappingMode     string `json:"mapping_mode,omitempty"`
	LastUpTime      string `json:"last_up_time,omitempty"`

	// Optik (dari `show ont optical-info` / `show ont postback optical-info`).
	RxPower       *float64 `json:"rx_power_dbm,omitempty"`
	TxPower       *float64 `json:"tx_power_dbm,omitempty"`
	Voltage       *float64 `json:"voltage_v,omitempty"`
	BiasCurrent   *float64 `json:"bias_current_ma,omitempty"`
	Temperature   *float64 `json:"temperature_c,omitempty"`
	OLTRxOntPower *float64 `json:"olt_rx_ont_power_dbm,omitempty"`
}

// Status returns status kanonik Fiber Doctor untuk ONT ini.
//
// Pemetaan mengikuti semantik C-Data:
//
//	run-state online  -> Online
//	run-state offline -> Offline
//
// _online_ berarti ONT terdaftar dan aktif secara administrative, tetapi
// optik tetap harus diperiksa lewat optical-info.
func (o ONTInfo) Status() string {
	switch strings.ToLower(o.RunState) {
	case "online":
		return "Online"
	case "offline":
		return "Offline"
	default:
		return "Unknown"
	}
}

// ONTInfoSummary hasil parsing `show ont info <port-id> all`.
type ONTInfoSummary struct {
	ONTs   []ONTInfo
	Totals *ONTInfoTotals
}

// ONTInfoTotals adalah baris ringkasan pada tabel `show ont info`.
type ONTInfoTotals struct {
	Total    int
	Online   int
	Deactive int
	Success  int
	Failed   int
}

// reONTInfoSummary mem-parsing satu baris tabel ringkasan registrasi ONT.
//
// Format (manual FD16xx v3.0, "Case 1: Check the registration status of all
// ONTs on the PON2 port"):
//
//	0/0 2 3 XSYNT00000001 Active Online success match
//
// kolom: F/S | Port | ONT-ID | SN | Control flag | Run state | Config state |
//
//	Match state | Last down time | Description
var reONTInfoSummary = regexp.MustCompile(
	`^\s*(\d+)/(\d+)\s+(\d+)\s+(\d+)\s+([A-Za-z0-9\-]+)\s+` +
		`(Active|Deactive)\s+(Online|Offline)\s+` +
		`(initial|failed|success)\s+(initial|match|mismatch)` +
		`(?:\s+([A-Za-z0-9\-]+))?\s*(.*)$`)

// reONTInfoTotals mem-parsing baris ringkasan:
//
//	Total: 3, online: 2, deactive: 0, success: 2, failed: 1
var reONTInfoTotals = regexp.MustCompile(
	`(?i)Total:\s*(\d+),\s*online:\s*(\d+),\s*deactive:\s*(\d+),\s*success:\s*(\d+)\s*,\s*failed:\s*(\d+)`)

// ParseONTInfoSummary mem-parsing output `show ont info <port-id> all`.
func ParseONTInfoSummary(raw string) ONTInfoSummary {
	out := ONTInfoSummary{}

	normalized := strings.ReplaceAll(raw, "\r", "")
	for _, line := range strings.Split(normalized, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || isTableSeparator(stripped) {
			continue
		}
		lower := strings.ToLower(stripped)
		if strings.HasPrefix(lower, "f/sp") || strings.HasPrefix(lower, "olt(") ||
			strings.HasPrefix(lower, "page ") || strings.HasPrefix(lower, "total:") {
			continue
		}

		m := reONTInfoSummary.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}

		frame, _ := strconv.Atoi(m[1])
		slot, _ := strconv.Atoi(m[2])
		port, _ := strconv.Atoi(m[3])
		ontID, _ := strconv.Atoi(m[4])

		out.ONTs = append(out.ONTs, ONTInfo{
			Frame:        frame,
			Slot:         slot,
			Port:         port,
			ONTID:        ontID,
			SN:           m[5],
			ControlFlag:  strings.ToLower(m[6]),
			RunState:     strings.ToLower(m[7]),
			ConfigState:  strings.ToLower(m[8]),
			MatchState:   strings.ToLower(m[9]),
			LastDownTime: m[10],
			Description:  strings.TrimSpace(m[11]),
		})
	}

	if m := reONTInfoTotals.FindStringSubmatch(normalized); m != nil {
		out.Totals = &ONTInfoTotals{
			Total:    atoiSafe(m[1]),
			Online:   atoiSafe(m[2]),
			Deactive: atoiSafe(m[3]),
			Success:  atoiSafe(m[4]),
			Failed:   atoiSafe(m[5]),
		}
	}

	return out
}

// reONTDetailField mem-parsing pasangan "Label : value" pada output detail.
var reONTDetailField = regexp.MustCompile(`(?i)^([A-Za-z][A-Za-z0-9 /()\-]*?)\s*:\s*(.*)$`)

// ParseONTInfoDetail mem-parsing output `show ont info <port-id> <ont-id>`
// atau `show ont info by-sn <sn>`.
//
// Contoh (manual FD16xx v3.0):
//
//	F/S : 0/0
//	Port : 2
//	ONT-ID : 3
//	Control flag : active
//	Run state : online
//	Config state : success
//	Match state : match
//	DBA type: SR
//	Distance(m) : 1
//	Validity mode : always
//	Authentic mode : sn-auth
//	SN : XSYNT00000001
func ParseONTInfoDetail(raw string) ONTInfo {
	out := ONTInfo{}

	normalized := strings.ReplaceAll(raw, "\r", "")
	for _, line := range strings.Split(normalized, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || isTableSeparator(stripped) {
			continue
		}

		m := reONTDetailField.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := strings.TrimSpace(m[2])

		switch key {
		case "f/s":
			if parts := strings.Split(val, "/"); len(parts) == 2 {
				out.Frame = atoiSafe(parts[0])
				out.Slot = atoiSafe(parts[1])
			}
		case "port":
			out.Port = atoiSafe(val)
		case "ont-id", "ont id":
			out.ONTID = atoiSafe(val)
		case "control flag":
			out.ControlFlag = strings.ToLower(val)
		case "run state":
			out.RunState = strings.ToLower(val)
		case "config state":
			out.ConfigState = strings.ToLower(val)
		case "match state":
			out.MatchState = strings.ToLower(val)
		case "dba type":
			out.DBAType = val
		case "distance(m)":
			out.DistanceM = atoiSafe(val)
		case "validity mode":
			out.ValidityMode = val
		case "authentic mode":
			out.AuthenticMode = val
		case "sn":
			out.SN = val
		case "description":
			out.Description = val
		case "last up time":
			out.LastUpTime = val
		case "last down time":
			out.LastDownTime = val
		case "line profile-id":
			out.LineProfileID = atoiSafe(val)
		case "line profile-name":
			out.LineProfileName = val
		case "service profile-name":
			out.ServiceProfile = val
		case "fec upstream":
			out.FECUpstream = val
		case "omcc encrypt":
			out.OMCCEncrypt = val
		case "qos mode":
			out.QoSMode = val
		case "mapping mode":
			out.MappingMode = val
		}
	}

	return out
}

// reOpticalField mem-parsing label optik C-Data:
//
//	Rx optical power(dBm) : -21.35
//	Voltage(V) : 3.28
var reOpticalField = regexp.MustCompile(
	`(?i)^(Voltage\(V\)|Tx optical power\(dBm\)|Rx optical power\(dBm\)|Laser bias current\(mA\)|Temperature\(C\)|OLT Rx ONT optical power\(dBm\))\s*:\s*([-+]?\d+(?:\.\d+)?)`)

// ParseONTOpticalInfo mem-parsing output:
//
//	show ont optical-info <port-id> <ont-id>
//	show ont postback optical-info <port-id> <ont-id>
//
// Contoh (manual FD16xx v3.0):
//
//	Frame/Slot : 0/0
//	Port : 2
//	ONT-ID : 3
//	Voltage(V) : 3.276
//	Tx optical power(dBm) : 2.310
//	Rx optical power(dBm) : -21.350
//	Laser bias current(mA) : 33.000
//	Temperature(C) : 41.000
func ParseONTOpticalInfo(raw string) ONTInfo {
	out := ONTInfo{}

	normalized := strings.ReplaceAll(raw, "\r", "")
	for _, line := range strings.Split(normalized, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}

		if m := reOpticalField.FindStringSubmatch(stripped); m != nil {
			v, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				continue
			}
			switch strings.ToLower(m[1]) {
			case "voltage(v)":
				out.Voltage = &v
			case "tx optical power(dbm)":
				out.TxPower = &v
			case "rx optical power(dbm)":
				out.RxPower = &v
			case "laser bias current(ma)":
				out.BiasCurrent = &v
			case "temperature(c)":
				out.Temperature = &v
			case "olt rx ont optical power(dbm)":
				out.OLTRxOntPower = &v
			}
			continue
		}

		m := reONTDetailField.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := strings.TrimSpace(m[2])
		switch key {
		case "frame/slot":
			if parts := strings.Split(val, "/"); len(parts) == 2 {
				out.Frame = atoiSafe(parts[0])
				out.Slot = atoiSafe(parts[1])
			}
		case "port":
			out.Port = atoiSafe(val)
		case "ont-id", "ont id":
			out.ONTID = atoiSafe(val)
		}
	}

	return out
}

// RegisterStatistics adalah satu baris `show ont register-statistics`.
type RegisterStatistics struct {
	Frame         int `json:"frame"`
	Slot          int `json:"slot"`
	Port          int `json:"port"`
	AutoFind      int `json:"autofind"`
	Authenticated int `json:"authenticated"`
	Online        int `json:"online"`
}

// reRegisterStats mem-parsing baris agregat register-statistics:
//
//	0/0        1         3         3         3
var reRegisterStats = regexp.MustCompile(`^(\d+)/(\d+)\s+(\d+)\s+(\d+)\s+(\d+)\s+(\d+)$`)

// ParseRegisterStatistics mem-parsing output `show ont register-statistics all`.
func ParseRegisterStatistics(raw string) []RegisterStatistics {
	var out []RegisterStatistics

	normalized := strings.ReplaceAll(raw, "\r", "")
	for _, line := range strings.Split(normalized, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || isTableSeparator(stripped) {
			continue
		}
		lower := strings.ToLower(stripped)
		if strings.HasPrefix(lower, "f/sp") || strings.HasPrefix(lower, "olt(") || strings.HasPrefix(lower, "page ") {
			continue
		}

		m := reRegisterStats.FindStringSubmatch(stripped)
		if m == nil {
			continue
		}
		out = append(out, RegisterStatistics{
			Frame:         atoiSafe(m[1]),
			Slot:          atoiSafe(m[2]),
			Port:          atoiSafe(m[3]),
			AutoFind:      atoiSafe(m[4]),
			Authenticated: atoiSafe(m[5]),
			Online:        atoiSafe(m[6]),
		})
	}

	return out
}

// ServiceStatus memetakan status service management OLT C-Data.
//
// Perintah: `show service status`
// Contoh keluaran: "telnet : enable", "ssh : disable"
type ServiceStatus struct {
	Services map[string]string `json:"services"`
}

// reServiceStatus mem-parsing baris status service.
var reServiceStatus = regexp.MustCompile(
	`(?i)^(telnet|ssh|http|https|snmp)\s*[:=]\s*(enable|disable|enabled|disabled)\b`)

// reServiceStatusTable mem-parsing varian tabel:
//
//	telnet   1   1   enable
var reServiceStatusTable = regexp.MustCompile(
	`(?i)^(telnet|ssh|http|https|snmp)\s+\d+\s+\d+\s+(enable|disable|enabled|disabled)\b`)

// reServiceStatusSimple mem-parsing varian sederhana:
//
//	telnet         enable
var reServiceStatusSimple = regexp.MustCompile(
	`(?i)^(telnet|ssh|http|https|snmp)\s+(enable|disable|enabled|disabled)\s*$`)

// ParseServiceStatus mem-parsing output `show service status`.
func ParseServiceStatus(raw string) ServiceStatus {
	out := ServiceStatus{Services: map[string]string{}}

	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}

		m := reServiceStatus.FindStringSubmatch(stripped)
		if m == nil {
			m = reServiceStatusTable.FindStringSubmatch(stripped)
		}
		if m == nil {
			m = reServiceStatusSimple.FindStringSubmatch(stripped)
		}
		if m == nil {
			continue
		}
		out.Services[strings.ToLower(m[1])] = normalizeEnable(m[2])
	}

	return out
}

// FailedConfigReason adalah satu komponen yang gagal dikonfigurasi.
type FailedConfigReason struct {
	Component string `json:"component"`
	State     string `json:"state"`
}

// ParseFailedConfig mem-parsing output `show ont failed-config <port> <ont>`.
func ParseFailedConfig(raw string) ([]FailedConfigReason, error) {
	var reasons []FailedConfigReason

	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || isTableSeparator(stripped) {
			continue
		}
		lower := strings.ToLower(stripped)
		if strings.HasPrefix(lower, "frame/slot") || strings.HasPrefix(lower, "port :") ||
			strings.HasPrefix(lower, "ont-id") || strings.HasPrefix(lower, "olt(") ||
			strings.HasPrefix(lower, "page ") {
			continue
		}
		if !strings.Contains(stripped, ":") {
			continue
		}
		parts := strings.SplitN(stripped, ":", 2)
		reasons = append(reasons, FailedConfigReason{
			Component: strings.TrimSpace(parts[0]),
			State:     strings.TrimSpace(parts[1]),
		})
	}

	if len(reasons) == 0 {
		return nil, fmt.Errorf("tidak ada komponen gagal konfigurasi pada output")
	}
	return reasons, nil
}

// isTableSeparator menentukan apakah baris hanya garis pemisah tabel.
func isTableSeparator(s string) bool {
	for _, r := range s {
		if r != '-' && r != ' ' && r != '_' {
			return false
		}
	}
	return true
}

func normalizeEnable(v string) string {
	if strings.Contains(strings.ToLower(v), "enabl") {
		return "enable"
	}
	return "disable"
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
