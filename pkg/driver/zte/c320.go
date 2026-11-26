package zte

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
)

func init() {
	driver.RegisterDriver("zte_c320", NewZTEC320Driver)
	driver.RegisterDriver("zte_c300", NewZTEC320Driver) // C300 CLI sintaks 95% identik
}

// ZTEC320Driver implementasi OLTDriver untuk ZTE C320 dan C300.
//
// Seluruh perintah dan format output di driver ini diverifikasi terhadap
// OLT-SYNTH-A (C320 Version V2.1.0) pada 2026-11-05. Lihat
// testdata/zte_c320_synth/olt-synth-a/ untuk output mentah aslinya.
type ZTEC320Driver struct {
	cfg    driver.OLTConfig
	telnet *TelnetClient
}

func NewZTEC320Driver(cfg driver.OLTConfig) (driver.OLTDriver, error) {
	port := cfg.TelnetPort
	if port <= 0 {
		port = 23
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 15
	}

	tc := &TelnetClient{
		Host:           cfg.Host,
		Port:           port,
		Username:       cfg.TelnetUser,
		Password:       cfg.TelnetPass,
		TimeoutSeconds: timeout,
	}

	return &ZTEC320Driver{
		cfg:    cfg,
		telnet: tc,
	}, nil
}

// GetSummary menyusun ringkasan kesehatan OLT dari data LIVE perangkat.
//
// Perintah yang dipakai (semua read-only):
//   - show system-group      -> model, firmware, uptime, lokasi
//   - show card              -> daftar kartu & status INSERVICE
//   - show card-temperature  -> suhu tiap kartu (maksimum = suhu OLT)
//   - show gpon onu state    -> agregat status ONU
func (d *ZTEC320Driver) GetSummary(ctx context.Context) (*driver.OLTSummary, error) {
	summary := &driver.OLTSummary{
		ID:         d.cfg.ID,
		Name:       d.cfg.Name,
		IP:         d.cfg.Host,
		Vendor:     "ZTE",
		Model:      d.cfg.Model,
		LastPolled: time.Now(),
	}

	// Identitas & versi perangkat.
	out, err := d.telnet.ExecuteCommand(ctx, "show system-group")
	if err != nil {
		return nil, fmt.Errorf("gagal membaca identitas sistem: %w", err)
	}
	sys := ParseZTESystemGroup(out)
	if sys.Firmware != "" {
		summary.Firmware = sys.Firmware
	}
	if sys.Model != "" {
		summary.Model = sys.Model
	}
	summary.Uptime = sys.Uptime
	summary.Location = sys.Location
	summary.SystemName = sys.SystemName

	// Kartu terpasang & statusnya.
	if out, err := d.telnet.ExecuteCommand(ctx, "show card"); err == nil {
		for _, card := range ParseZTECard(out) {
			summary.Cards++
			if strings.EqualFold(card.Status, "INSERVICE") {
				summary.CardsInService++
			}
		}
	}

	// Suhu maksimum seluruh kartu aktif.
	if out, err := d.telnet.ExecuteCommand(ctx, "show card-temperature"); err == nil {
		for _, temp := range ParseZTECardTemperature(out) {
			if temp > summary.TemperatureC {
				summary.TemperatureC = temp
			}
		}
	}

	// Agregat status ONU.
	out, err = d.telnet.ExecuteCommand(ctx, "show gpon onu state")
	if err != nil {
		return nil, fmt.Errorf("gagal membaca state ONU: %w", err)
	}
	records, err := ParseZTEONUState(out, d.cfg.Name)
	if err != nil {
		return nil, err
	}
	summary.TotalONUs = len(records)
	for _, rec := range records {
		switch rec.Status {
		case "Online":
			summary.OnlineONUs++
		case "Offline":
			summary.OfflineONUs++
		case "LOS":
			summary.LosONUs++
			summary.CriticalONUs++
		case "DyingGasp":
			summary.DyingGaspONUs++
			summary.CriticalONUs++
		}
	}

	return summary, nil
}

// ListONUs menarik inventaris ONU lengkap.
//
// Sumber data digabung dari dua perintah:
//   - `show gpon onu state`    -> status real-time (phase/OMCC/admin)
//   - `show running-config`    -> Serial Number, nama pelanggan, VLAN, T-CONT
//
// Catatan: tabel state TIDAK memuat SN, jadi running-config wajib diambil.
func (d *ZTEC320Driver) ListONUs(ctx context.Context) ([]driver.ONURecord, error) {
	stateOut, err := d.telnet.ExecuteCommand(ctx, "show gpon onu state")
	if err != nil {
		return nil, fmt.Errorf("gagal mengeksekusi 'show gpon onu state': %w", err)
	}

	records, err := ParseZTEONUState(stateOut, d.cfg.Name)
	if err != nil {
		return nil, err
	}

	cfgOut, err := d.telnet.ExecuteCommand(ctx, "show running-config")
	if err != nil {
		// Inventaris status tetap berguna walau running-config gagal diambil.
		return records, nil
	}

	registry := ParseZTERegistry(cfgOut)
	details := ParseZTERunningConfig(cfgOut, d.cfg.Name)
	byID := make(map[string]driver.ONURecord, len(details))
	for _, rec := range details {
		byID[rec.ONUID] = rec
	}

	for i := range records {
		if entry, ok := registry[records[i].ONUID]; ok {
			records[i].SN = entry.SN
			if records[i].DeviceType == "" {
				records[i].DeviceType = entry.DeviceType
			}
		}
		if detail, ok := byID[records[i].ONUID]; ok {
			records[i].CustomerName = detail.CustomerName
			records[i].Description = detail.Description
			records[i].TContProfile = detail.TContProfile
			records[i].ServiceVLANs = detail.ServiceVLANs
		}
	}

	return records, nil
}

// GetONULive mengambil detail lengkap + telemetri optik satu ONU.
//
//   - `show gpon onu detail-info gpon-onu_X/Y/Z:N` -> identitas & state
//   - `show pon power attenuation gpon-onu_X/Y/Z:N` -> daya optik (dBm)
func (d *ZTEC320Driver) GetONULive(ctx context.Context, sn string) (*driver.ONURecord, error) {
	if strings.TrimSpace(sn) == "" {
		return nil, fmt.Errorf("SN ONU kosong")
	}

	// Resolusi SN -> interface ONU lewat running-config.
	cfgOut, err := d.telnet.ExecuteCommand(ctx, "show running-config")
	if err != nil {
		return nil, fmt.Errorf("gagal membaca running-config: %w", err)
	}
	target := ""
	for onuID, entry := range ParseZTERegistry(cfgOut) {
		if strings.EqualFold(entry.SN, sn) {
			target = onuID
			break
		}
	}
	if target == "" {
		return nil, fmt.Errorf("ONU dengan SN '%s' tidak ditemukan di running-config", sn)
	}

	iface := "gpon-onu_" + target
	detailOut, err := d.telnet.ExecuteCommand(ctx, "show gpon onu detail-info "+iface)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil detail-info %s: %w", iface, err)
	}
	rec, err := ParseZTEDetailInfo(detailOut, d.cfg.Name)
	if err != nil {
		return nil, err
	}

	if powerOut, err := d.telnet.ExecuteCommand(ctx, "show pon power attenuation "+iface); err == nil {
		power := ParseZTEOpticalPower(powerOut)
		rec.RxOLT = power.OLTRxDBm
		rec.RxONU = power.ONURxDBm
	}

	return &rec, nil
}

// GetOpticalPowerBulk mengambil redaman optik SELURUH ONU pada satu port PON
// dengan 2 panggilan CLI.
//
// _Oleh karena itu berlawanan dengan memanggil `show pon power attenuation`
// per ONU (1 panggilan × N ONU), perintah ini:
//   - show pon power olt-rx gpon-olt_<port>   -> daya diterima OLT (kunci SOP)
//   - show pon power onu-rx gpon-olt_<port>   -> daya diterima ONU
//
// ponPort berformat "slot/subslot/port", contoh "1/2/15".
func (d *ZTEC320Driver) GetOpticalPowerBulk(ctx context.Context, ponPort string) (map[string]ONUOptical, error) {
	ponPort = strings.TrimSpace(ponPort)
	if ponPort == "" {
		return nil, fmt.Errorf("port PON kosong")
	}
	iface := "gpon-olt_" + ponPort

	oltRxOut, err := d.telnet.ExecuteCommand(ctx, "show pon power olt-rx "+iface)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca olt-rx %s: %w", iface, err)
	}
	onuRxOut, err := d.telnet.ExecuteCommand(ctx, "show pon power onu-rx "+iface)
	if err != nil {
		return nil, fmt.Errorf("gagal membaca onu-rx %s: %w", iface, err)
	}

	oltRx := ParseZTEOpticalPowerBulk(oltRxOut, "olt-rx")
	onuRx := ParseZTEOpticalPowerBulk(onuRxOut, "onu-rx")

	result := make(map[string]ONUOptical, len(oltRx))
	for onuID, reading := range oltRx {
		result[onuID] = ONUOptical{
			OnuID:    onuID,
			OLTRx:    reading.DBm,
			NoSignal: !reading.HasSignal,
		}
	}
	for onuID, reading := range onuRx {
		entry := result[onuID]
		entry.OnuID = onuID
		entry.ONURx = reading.DBm
		result[onuID] = entry
	}

	return result, nil
}

// GetUnconfiguredONUs mendeteksi modem yang terpasang di splitter namun belum
// diotorisasi.
//
// Format live:
//
//	OnuIndex                 Sn                  State
//	---------------------------------------------------------------------
//	gpon-olt_1/2/15:1        HWTC86F1BDB4        unknown
func (d *ZTEC320Driver) GetUnconfiguredONUs(ctx context.Context) ([]driver.UnconfiguredONU, error) {
	out, err := d.telnet.ExecuteCommand(ctx, "show gpon onu uncfg")
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil unconfigured ONU: %w", err)
	}

	var results []driver.UnconfiguredONU
	re := regexp.MustCompile(`(?i)(gpon-olt_[\d/]+):(\d+)\s+(\S+)\s+(\S+)`)
	for _, m := range re.FindAllStringSubmatch(out, -1) {
		sn := strings.TrimSpace(m[3])
		if len(sn) < 4 {
			continue
		}
		results = append(results, driver.UnconfiguredONU{
			OLT:        d.cfg.Name,
			PONPort:    m[1],
			SN:         sn,
			VendorID:   strings.ToUpper(sn[:4]),
			DetectedAt: time.Now(),
		})
	}
	return results, nil
}

// AuthorizeONU mem-provisi modem baru sesuai SOP kanonik referensi.
//
// Perintah dijalankan SATU PER SATU (bukan dilumpukkan) agar output tiap
// langkah dapat diverifikasi dan tidak membanjiri buffer kontroler C320.
//
// Catatan keselamatan: `write` menyimpan konfigurasi ke flash OLT.
func (d *ZTEC320Driver) AuthorizeONU(ctx context.Context, req driver.AuthorizeRequest) error {
	deviceType := req.ActualType
	if deviceType == "" {
		deviceType = "ALL"
	}

	script := []string{
		"configure terminal",
		fmt.Sprintf("interface gpon-olt_%s", req.PONPort),
		fmt.Sprintf("onu %d type %s sn %s", req.Index, deviceType, req.SN),
		"exit",
		fmt.Sprintf("interface gpon-onu_%s:%d", req.PONPort, req.Index),
		fmt.Sprintf("name %s", req.CustomerName),
		fmt.Sprintf("description %s", req.ODPName),
		"sn-bind enable sn",
		fmt.Sprintf("tcont 1 profile %s", req.TContProfile),
		"gemport 1 tcont 1",
		fmt.Sprintf("service-port 1 vport 1 user-vlan %d vlan %d", req.ServiceVLAN, req.ServiceVLAN),
		"exit",
		fmt.Sprintf("pon-onu-mng gpon-onu_%s:%d", req.PONPort, req.Index),
		fmt.Sprintf("service 1 gemport 1 vlan %d", req.ServiceVLAN),
		"exit",
		"exit",
		"write",
	}

	if _, err := d.telnet.ExecuteScript(ctx, script); err != nil {
		return fmt.Errorf("otorisasi ONU %s gagal: %w", req.SN, err)
	}
	return nil
}

// ExecuteRawCLI mengeksekusi perintah CLI mentah (read-only maupun mutasi).
func (d *ZTEC320Driver) ExecuteRawCLI(ctx context.Context, cmd string) (string, error) {
	return d.telnet.ExecuteCommand(ctx, cmd)
}
