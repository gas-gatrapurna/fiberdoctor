package driver

import (
	"context"
	"time"
)

// OLTSummary merepresentasikan kondisi kesehatan perangkat fisik OLT.
type OLTSummary struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	IP           string    `json:"ip"`
	Vendor       string    `json:"vendor"`
	Model        string    `json:"model"`
	Firmware     string    `json:"firmware"`
	Uptime       string    `json:"uptime"`
	TemperatureC float64   `json:"temperature_c"`
	TotalONUs    int       `json:"total_onus"`
	OnlineONUs   int       `json:"online_onus"`
	OfflineONUs  int       `json:"offline_onus"`
	CriticalONUs int       `json:"critical_onus"`
	LastPolled   time.Time `json:"last_polled"`

	// Field tambahan hasil ground-truth parsing OLT ZTE C320.
	SystemName     string `json:"system_name,omitempty"`
	Location       string `json:"location,omitempty"`
	Cards          int    `json:"cards"`
	CardsInService int    `json:"cards_in_service"`
	LosONUs        int    `json:"los_onus"`
	DyingGaspONUs  int    `json:"dying_gasp_onus"`
}

// PONLocation merepresentasikan alamat fisik port optik.
type PONLocation struct {
	Slot    int `json:"slot"`
	Subslot int `json:"subslot"`
	Port    int `json:"port"`
	Index   int `json:"index"`
}

// ONURecord merepresentasikan data inventaris & status operasional 1 modem/ONT.
//
// Catatan ground-truth OLT ZTE C320 (diverifikasi live 2026-11-05):
//   - `show gpon onu state` hanya memuat OnuIndex/Admin/OMCC/Phase/Channel —
//     TIDAK memuat Serial Number. SN harus diambil dari `show running-config`.
//   - AdminState = status administrasi (enable/disable).
//   - OMCCState  = status sesi OMCC (enable/disable) -> reachability modem.
//   - PhaseState = status optik/phase (working/LOS/OffLine/DyingGasp).
type ONURecord struct {
	OLT          string      `json:"olt"`
	ONUID        string      `json:"onu_id"`
	PON          PONLocation `json:"pon"`
	CustomerName string      `json:"customer_name"`
	Description  string      `json:"description"`
	ODPName      string      `json:"odp_name"`
	AuthDate     string      `json:"auth_date"`
	PPPoE        string      `json:"pppoe,omitempty"`
	SN           string      `json:"sn"`
	Status       string      `json:"status"` // Online, LOS, Offline, DyingGasp
	RxOLT        *float64    `json:"rx_olt_dbm,omitempty"`
	RxONU        *float64    `json:"rx_onu_dbm,omitempty"`
	ActualType   string      `json:"actual_type"`
	LastUpdated  time.Time   `json:"last_updated"`

	// Kolom tambahan hasil ground-truth parsing OLT ZTE.
	AdminState   string `json:"admin_state,omitempty"`
	OMCCState    string `json:"omcc_state,omitempty"`
	PhaseState   string `json:"phase_state,omitempty"`
	ONUState     string `json:"onu_state,omitempty"`
	Channel      string `json:"channel,omitempty"`
	DeviceType   string `json:"device_type,omitempty"`
	TContProfile string `json:"tcont_profile,omitempty"`
	ServiceVLANs []int  `json:"service_vlans,omitempty"`
	DistanceM    int    `json:"distance_m,omitempty"`
}

// UnconfiguredONU merepresentasikan modem baru yang terdeteksi di splitter tapi belum diotorisasi.
type UnconfiguredONU struct {
	OLT        string    `json:"olt"`
	PONPort    string    `json:"pon_port"` // Misal "1/2/15"
	SN         string    `json:"sn"`
	VendorID   string    `json:"vendor_id"`
	Model      string    `json:"model"`
	DetectedAt time.Time `json:"detected_at"`
}

// AuthorizeRequest parameter untuk provisi modem baru ke OLT.
type AuthorizeRequest struct {
	OLT          string `json:"olt"`
	PONPort      string `json:"pon_port"`
	Index        int    `json:"index,omitempty"`
	SN           string `json:"sn"`
	CustomerName string `json:"customer_name"`
	ODPName      string `json:"odp_name"`
	ServiceVLAN  int    `json:"service_vlan"`
	SpeedProfile string `json:"speed_profile"`
	TContProfile string `json:"tcont_profile"`
	ActualType   string `json:"actual_type,omitempty"` // Tipe ONU di OLT, mis. "ALL" atau "V2801RGW"
}

// OLTConfig konfigurasi kredensial & koneksi ke OLT.
type OLTConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Vendor         string `json:"vendor"` // zte, huawei, cdata
	Model          string `json:"model"`
	Host           string `json:"host"`
	TelnetPort     int    `json:"telnet_port"`
	TelnetUser     string `json:"telnet_user"`
	TelnetPass     string `json:"telnet_pass"`
	SSHPort        int    `json:"ssh_port"`
	SSHUser        string `json:"ssh_user"`
	SSHPass        string `json:"ssh_pass"`
	SNMPPort       int    `json:"snmp_port"`
	SNMPCommunity  string `json:"snmp_community"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// ONUOptical adalah gabungan pembacaan daya optik satu ONU dari beberapa arah.
//
// OLTRx adalah daya yang diterima OLT dari ONU — inilah nilai yang dipakai
// untuk klasifikasi SOP redaman referensi (gold standard -18 s/d -24 dBm).
type ONUOptical struct {
	OnuID    string   // "1/2/15:10"
	OLTRx    *float64 // daya diterima OLT dari ONU (kunci klasifikasi SOP)
	ONURx    *float64 // daya diterima ONU dari OLT
	NoSignal bool     // true bila OLT tidak menerima cahaya dari ONU
}

// OpticalBulkReader adalah kemampuan OPSIONAL sebuah driver untuk membaca
// redaman optik SELURUH ONU pada satu port PON dalam 2 panggilan CLI.
//
// Ini menggantikan pendekatan 1 panggilan per ONU yang membebani kartu
// kontroler OLT (satu perintah per ONU pada C320).
type OpticalBulkReader interface {
	GetOpticalPowerBulk(ctx context.Context, ponPort string) (map[string]ONUOptical, error)
}

// OLTDriver adalah antarmuka tunggal seragam untuk semua vendor OLT.
type OLTDriver interface {
	// Ringkasan kesehatan OLT (suhu, uptime, total ONU)
	GetSummary(ctx context.Context) (*OLTSummary, error)

	// Tarik seluruh daftar inventaris ONU terotorisasi
	ListONUs(ctx context.Context) ([]ONURecord, error)

	// Tarik telemetri detail 1 ONU secara live (redaman, status optik)
	GetONULive(ctx context.Context, sn string) (*ONURecord, error)

	// Deteksi modem baru yang belum di-authorize di splitter
	GetUnconfiguredONUs(ctx context.Context) ([]UnconfiguredONU, error)

	// Otorisasi & provisi profil VLAN ke modem baru
	AuthorizeONU(ctx context.Context, req AuthorizeRequest) error

	// Eksekusi perintah CLI langsung (raw CLI passthrough)
	ExecuteRawCLI(ctx context.Context, cmd string) (string, error)
}
