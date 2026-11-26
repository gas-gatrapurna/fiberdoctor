package cdata

import (
	"regexp"
	"sort"
	"strings"
)

// Family adalah keluarga produk C-Data.
type Family string

const (
	FamilyFD11 Family = "FD11" // seri EPON lama
	FamilyFD12 Family = "FD12" // seri EPON/GPON hybrid
	FamilyFD16 Family = "FD16" // seri GPON access (FD16xx) - yang paling umum di pasar FTTH
	FamilyFD6X Family = "FD6X" // FD602XW - EPON + GPON hybrid
	FamilyFD7G Family = "FD7G" // FD704G(W)(D)
	FamilyFD8G Family = "FD8G" // FD804G(W)(D)
)

// ModelProfile adalah profil kapabilitas untuk satu varian OLT C-Data.
//
// Profil ini dipakai untuk:
//  1. Menentukan perintah mana yang didukung varian tersebut.
//  2. Menyusun rantai fallback perintah (C-Data & ZTE sama-sama bisa
//     menolak sebuah perintah dengan "%Error"/"Invalid").
//  3. Menjelaskan keterbatasan yang KNOWN agar engine tidak diam-diam
//     melaporkan data yang tidak ada.
type ModelProfile struct {
	Vendor      string
	Model       string
	Family      Family
	DisplayName string

	// GPONPorts adalah jumlah port GPON yang terpasang. 0 berarti "tidak
	// diverifikasi" — jangan diasumsikan, enumerate dari perangkat.
	GPONPorts int
	// EPONPorts adalah jumlah port EPON. 0 berarti tidak diverifikasi.
	EPONPorts int

	// SupportsONTBulkList menandai perangkat menyediakan daftar ONT massal
	// per port (`show ont info <port> all`).
	SupportsONTBulkList bool
	// SupportsOpticalPerONT menandai perangkat menyediakan optik per ONT
	// (`show ont optical-info`). Tidak ada varian C-Data yang menyediakan
	// optik BULK per port seperti ZTE `show pon power olt-rx`.
	SupportsOpticalPerONT bool
	// SupportsSNLookup menandai `show ont info by-sn <sn>` tersedia.
	SupportsSNLookup bool
	// SupportsAutofind menandai `show ont autofind` tersedia (ONT belum
	// terdaftar / baru ditemukan).
	SupportsAutofind bool
	// SupportsRegisterStats menandai `show ont register-statistics` tersedia.
	SupportsRegisterStats bool
}

// modelRule mendefinisikan cara mengenali satu varian dari nama model.
type modelRule struct {
	re       *regexp.Regexp
	profile  ModelProfile
	priority int
}

// modelRules disusun dari YANG SPESIFIK ke YANG GENERIK agar
// "FD1602S-B1" tidak tertangkap aturan "FD16".
var modelRules = []modelRule{
	{
		re: regexp.MustCompile(`(?i)^FD1601`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD1601S-B1", Family: FamilyFD16,
			DisplayName: "C-Data FD1601S-B1 (1 PON)", GPONPorts: 1,
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD1602`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD1602S-B1", Family: FamilyFD16,
			DisplayName: "C-Data FD1602S-B1 (2 PON)", GPONPorts: 2,
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD1604`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD1604S-B1", Family: FamilyFD16,
			DisplayName: "C-Data FD1604S-B1 (4 PON)", GPONPorts: 4,
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD1608`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD1608S-B1", Family: FamilyFD16,
			DisplayName: "C-Data FD1608S-B1 (8 PON)", GPONPorts: 8,
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD1612`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD1612S-B1", Family: FamilyFD16,
			DisplayName: "C-Data FD1612S-B1 (12 PON)", GPONPorts: 12,
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD1616`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD1616S-B1", Family: FamilyFD16,
			DisplayName: "C-Data FD1616S-B1 (16 PON)", GPONPorts: 16,
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD16`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD16xx", Family: FamilyFD16,
			DisplayName: "C-Data FD16xx (seri GPON)",
		},
		priority: 5,
	},
	{
		re: regexp.MustCompile(`(?i)^FD602`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD602XW", Family: FamilyFD6X,
			DisplayName: "C-Data FD602XW (hybrid EPON+GPON)",
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD704`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD704G(W)(D)", Family: FamilyFD7G,
			DisplayName: "C-Data FD704G(W)(D) (GPON)",
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD804`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD804G(W)(D)", Family: FamilyFD8G,
			DisplayName: "C-Data FD804G(W)(D) (GPON)",
		},
		priority: 10,
	},
	{
		re: regexp.MustCompile(`(?i)^FD12`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD12xx", Family: FamilyFD12,
			DisplayName: "C-Data FD12xx (seri EPON/GPON)",
		},
		priority: 5,
	},
	{
		re: regexp.MustCompile(`(?i)^FD11`),
		profile: ModelProfile{
			Vendor: "cdata", Model: "FD11xx", Family: FamilyFD11,
			DisplayName: "C-Data FD11xx (seri EPON)",
		},
		priority: 5,
	},
}

// ResolveModel mengenali varian C-Data dari nama model (case-insensitive).
//
// Menerima juga nama file versi firmware yang panjang, mis.
// "FD1602S_B1_Pack_IW_V3.1.22_241031_3920_X000.img".
func ResolveModel(model string) (ModelProfile, bool) {
	name := strings.TrimSpace(model)
	if name == "" {
		return ModelProfile{}, false
	}

	// Buang ekstensi & sufiks versi agar pencocokan hanya melihat prefiks model.
	name = strings.TrimSuffix(name, ".img")
	name = strings.TrimSuffix(name, ".tar")
	name = strings.TrimSuffix(name, ".cfg")

	var best *modelRule
	for i := range modelRules {
		if modelRules[i].re.MatchString(name) {
			if best == nil || modelRules[i].priority > best.priority {
				best = &modelRules[i]
			}
		}
	}
	if best == nil {
		return ModelProfile{}, false
	}

	profile := best.profile
	profile.SupportsONTBulkList = profile.Family == FamilyFD16
	profile.SupportsOpticalPerONT = profile.Family == FamilyFD16
	profile.SupportsSNLookup = profile.Family == FamilyFD16
	profile.SupportsAutofind = profile.Family == FamilyFD16
	profile.SupportsRegisterStats = profile.Family == FamilyFD16
	return profile, true
}

// KnownModels mengembalikan daftar varian yang dikenali (untuk dokumentasi
// dan self-check driver).
func KnownModels() []string {
	seen := map[string]bool{}
	var out []string
	for _, rule := range modelRules {
		if !seen[rule.profile.Model] {
			seen[rule.profile.Model] = true
			out = append(out, rule.profile.Model)
		}
	}
	sort.Strings(out)
	return out
}

// CommandSet berisi perintah CLI C-Data beserta rantai fallback.
//
// Fallback dipakai karena C-Data (dan ZTE) menolak perintah yang tidak
// didukung dengan pesan "%Error"/"Invalid command". Rantai fallback membuat
// driver tetap berfungsi pada varian lama (FD11/FD12) maupun FD16xx.
type CommandSet struct {
	// ServiceStatus mendeteksi service management yang aktif.
	ServiceStatus []string
	// ONTList = daftar ONT pada satu port PON.
	ONTList []string
	// ONTDetail = detail satu ONT.
	ONTDetail []string
	// ONTBySN = lookup ONT berdasarkan serial number.
	ONTBySN []string
	// OpticalInfo = optik satu ONT.
	OpticalInfo []string
	// PostbackOptical = sisi OLT menerima cahaya dari ONT.
	PostbackOptical []string
	// RegisterStatistics = agregat registrasi per port.
	RegisterStatistics []string
	// Autofind = ONT auto-discovered namun belum terdaftar.
	Autofind []string
	// FailedConfig = diagnosis kegagalan konfigurasi ONT.
	FailedConfig []string
	// PortState = status link port ethernet ONT.
	PortState []string
	// DeviceInfo = identitas perangkat.
	DeviceInfo []string
}

// FD16CommandSet adalah set perintah untuk seri FD16xx (diverifikasi dari
// manual CLI resmi FD16xx v3.0).
var FD16CommandSet = CommandSet{
	ServiceStatus:      []string{"show service status"},
	ONTList:            []string{"show ont info %s all"},
	ONTDetail:          []string{"show ont info %s %s"},
	ONTBySN:            []string{"show ont info by-sn %s"},
	OpticalInfo:        []string{"show ont optical-info %s %s"},
	PostbackOptical:    []string{"show ont postback optical-info %s %s"},
	RegisterStatistics: []string{"show ont register-statistics all"},
	Autofind:           []string{"show ont autofind %s all"},
	FailedConfig:       []string{"show ont failed-config %s %s"},
	PortState:          []string{"show ont port state %s %s eth all"},
	DeviceInfo:         []string{"show version", "show device", "show system information"},
}

// CommandSetFor mengembalikan set perintah yang sesuai untuk sebuah profil.
//
// Untuk keluarga non-FD16 (FD11/FD12/FD6X/FD7G/FD8G) memakai set FD16
// sebagai basis, karena sintaks `show ont ...` berasal dari platform OLT
// yang sama; namun flag kapabilitas pada profil tetap controlling sehingga
// driver tidak mengklaim sesuatu yang tidak terverifikasi.
func CommandSetFor(profile ModelProfile) CommandSet {
	return FD16CommandSet
}
