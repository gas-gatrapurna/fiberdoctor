// Package cdata — driver OLT untuk perangkat C-Data (Shenzhen C-Data
// Technology) seluruh varian FD.
//
// Ground truth:
//   - Manual CLI resmi FD16xx v3.0 (2022-09-01)
//   - Adapter Python referensi yang berjalan di produksi (tidak dipublikasikan)
//
// C-Data memakai terminologi ONT (bukan ONU) dan sintaks CLI berbeda dari
// ZTE: perintah inventaris adalah `show ont info <port> all` dan tabelnya
// MEMUAT kolom SN.
package cdata

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver/cli"
)

func init() {
	// Daftarkan seluruh varian C-Data yang dikenali. Semua memakai driver
	// yang sama; pembeda handled oleh ModelProfile.
	for _, model := range KnownModels() {
		driver.RegisterDriver("cdata_"+strings.ToLower(model), NewCDataDriver)
	}
	driver.RegisterDriver("cdata", NewCDataDriver)
}

// CDataDriver implementasi driver.OLTDriver untuk OLT C-Data.
type CDataDriver struct {
	cfg     driver.OLTConfig
	profile ModelProfile
	client  *cli.Client
	cmds    CommandSet
}

// NewCDataDriver membuat driver C-Data.
//
// cfg.Model dipakai untuk menentukan profil varian (jumlah PON, dukungan
// perintah). Bila model tidak dikenali, profil default FD16xx dipakai dan
// jumlah PON dibiarkan 0 (harus di-enumerate dari perangkat, bukan diasumsikan).
func NewCDataDriver(cfg driver.OLTConfig) (driver.OLTDriver, error) {
	profile, ok := ResolveModel(cfg.Model)
	if !ok {
		profile = ModelProfile{
			Vendor:      "cdata",
			Model:       cfg.Model,
			Family:      FamilyFD16,
			DisplayName: "C-Data " + cfg.Model,
		}
	}
	profile.SupportsONTBulkList = profile.Family == FamilyFD16
	profile.SupportsOpticalPerONT = profile.Family == FamilyFD16
	profile.SupportsSNLookup = profile.Family == FamilyFD16
	profile.SupportsAutofind = profile.Family == FamilyFD16
	profile.SupportsRegisterStats = profile.Family == FamilyFD16

	port := cfg.TelnetPort
	if port <= 0 {
		port = 23
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 15
	}

	return &CDataDriver{
		cfg:     cfg,
		profile: profile,
		cmds:    CommandSetFor(profile),
		client: cli.New(cli.Options{
			Host:                 cfg.Host,
			Port:                 port,
			Username:             cfg.TelnetUser,
			Password:             cfg.TelnetPass,
			TimeoutSeconds:       timeout,
			UsernamePrompts:      []string{"user name:", "username:", "login:"},
			PasswordPrompts:      []string{"password:"},
			EnableCommand:        "enable",
			EnablePasswordPrompt: "password:",
			PromptRe:             cdataPromptRe,
			DisablePagingCommand: "terminal length 0",
		}),
	}, nil
}

// cdataPromptRe mengenali baris prompt CLI C-Data.
//
// Contoh prompt C-Data (dari manual FD16xx):
//
//	OLT#
//	OLT(config)#
//	OLT(config-interface-gpon-0/0)#
var cdataPromptRe = regexp.MustCompile(`(?m)^[A-Za-z][A-Za-z0-9_.\-]*(\([a-z\-0-9/ ]+\))?[#>]\s*$`)

// Profile mengembalikan profil kapabilitas varian yang dipakai driver.
func (d *CDataDriver) Profile() ModelProfile { return d.profile }

// GetSummary menyusun ringkasan kesehatan OLT C-Data.
//
// Perintah yang dipakai (read-only):
//   - show service status      -> service management aktif
//   - show ont register-statistics all -> agregat registrasi per port
func (d *CDataDriver) GetSummary(ctx context.Context) (*driver.OLTSummary, error) {
	summary := &driver.OLTSummary{
		ID:         d.cfg.ID,
		Name:       d.cfg.Name,
		IP:         d.cfg.Host,
		Vendor:     "C-Data",
		Model:      d.profile.Model,
		LastPolled: time.Now(),
	}

	if out, _, err := cli.FirstSupported(ctx, d.client, d.cmds.DeviceInfo); err == nil {
		summary.Firmware = firstMeaningfulLine(out)
	}

	// Agregat registrasi per port.
	if out, _, err := cli.FirstSupported(ctx, d.client, d.cmds.RegisterStatistics); err == nil {
		for _, stat := range ParseRegisterStatistics(out) {
			summary.TotalONUs += stat.Authenticated
			summary.OnlineONUs += stat.Online
		}
	}

	// Status service management (telnet/ssh/http/https/snmp).
	if out, _, err := cli.FirstSupported(ctx, d.client, d.cmds.ServiceStatus); err == nil {
		svc := ParseServiceStatus(out)
		for _, state := range svc.Services {
			if state == "enable" {
				summary.CardsInService++
			}
		}
	}

	return summary, nil
}

// ListONUs menarik inventaris ONT lengkap.
//
// Sumber data: `show ont info <port-id> all` untuk setiap port PON.
func (d *CDataDriver) ListONUs(ctx context.Context) ([]driver.ONURecord, error) {
	ports, err := d.ListPONPorts(ctx)
	if err != nil {
		return nil, err
	}

	var records []driver.ONURecord
	for _, port := range ports {
		cmd := formatFirst(d.cmds.ONTList, port)
		out, _, err := cli.FirstSupported(ctx, d.client, []string{cmd})
		if err != nil {
			continue
		}

		summary := ParseONTInfoSummary(out)
		for _, ont := range summary.ONTs {
			records = append(records, d.toONURecord(ont))
		}
	}

	return records, nil
}

// GetONULive mengambil detail + optik satu ONT berdasarkan SN.
func (d *CDataDriver) GetONULive(ctx context.Context, sn string) (*driver.ONURecord, error) {
	if strings.TrimSpace(sn) == "" {
		return nil, fmt.Errorf("SN ONT kosong")
	}

	if !d.profile.SupportsSNLookup {
		return nil, fmt.Errorf("lookup by-SN tidak tersedia untuk varian %s", d.profile.Model)
	}

	cmd := formatFirst(d.cmds.ONTBySN, sn)
	out, _, err := cli.FirstSupported(ctx, d.client, []string{cmd})
	if err != nil {
		return nil, fmt.Errorf("gagal lookup ONT %s: %w", sn, err)
	}

	detail := ParseONTInfoDetail(out)
	if detail.SN == "" {
		return nil, fmt.Errorf("SN %s tidak ditemukan di perangkat", sn)
	}

	rec := d.toONURecord(detail)

	// Optik per ONT (C-Data tidak punya mode bulk seperti ZTE).
	optCmd := formatFirst(d.cmds.OpticalInfo, strconv.Itoa(detail.Port), strconv.Itoa(detail.ONTID))
	if optOut, _, err := cli.FirstSupported(ctx, d.client, []string{optCmd}); err == nil {
		optical := ParseONTOpticalInfo(optOut)
		rec.RxOLT = optical.RxPower
		rec.RxONU = optical.OLTRxOntPower
	}

	return &rec, nil
}

// GetUnconfiguredONUs mendeteksi ONT yang ditemukan otomatis namun belum
// terdaftar (menggantikan konsep "unconfigured ONU" pada ZTE).
func (d *CDataDriver) GetUnconfiguredONUs(ctx context.Context) ([]driver.UnconfiguredONU, error) {
	if !d.profile.SupportsAutofind {
		return nil, fmt.Errorf("perintah autofind tidak tersedia untuk varian %s", d.profile.Model)
	}

	ports, err := d.ListPONPorts(ctx)
	if err != nil {
		return nil, err
	}

	var results []driver.UnconfiguredONU
	for _, port := range ports {
		cmd := formatFirst(d.cmds.Autofind, port)
		out, _, err := cli.FirstSupported(ctx, d.client, []string{cmd})
		if err != nil {
			continue
		}
		for _, sn := range parseAutofindSNs(out) {
			results = append(results, driver.UnconfiguredONU{
				OLT:        d.cfg.Name,
				PONPort:    port,
				SN:         sn,
				VendorID:   vendorPrefix(sn),
				DetectedAt: time.Now(),
			})
		}
	}

	return results, nil
}

// ListPONPorts mengembalikan daftar port PON aktif.
//
// Bila jumlah port tidak terverifikasi pada profil, daftar diambil dari
// agregat `show ont register-statistics all`; bila itu kosong, fallback ke
// enumerasi 1..maxPorts dengan verifikasi per port.
func (d *CDataDriver) ListPONPorts(ctx context.Context) ([]string, error) {
	if out, _, err := cli.FirstSupported(ctx, d.client, d.cmds.RegisterStatistics); err == nil {
		stats := ParseRegisterStatistics(out)
		if len(stats) > 0 {
			seen := map[string]bool{}
			var ports []string
			for _, s := range stats {
				key := fmt.Sprintf("%d/%d/%d", s.Frame, s.Slot, s.Port)
				if !seen[key] {
					seen[key] = true
					ports = append(ports, key)
				}
			}
			return ports, nil
		}
	}

	// Fallback: enumerasi berdasarkan jumlah port pada profil.
	maxPorts := d.profile.GPONPorts
	if maxPorts <= 0 {
		maxPorts = 16
	}
	var ports []string
	for p := 1; p <= maxPorts; p++ {
		cmd := formatFirst(d.cmds.ONTList, strconv.Itoa(p))
		out, _, err := cli.FirstSupported(ctx, d.client, []string{cmd})
		if err != nil {
			continue
		}
		if len(ParseONTInfoSummary(out).ONTs) > 0 {
			ports = append(ports, strconv.Itoa(p))
		}
	}
	return ports, nil
}

// AuthorizeONU mendaftarkan ONT baru ke OLT C-Data.
//
// Perintah dijalankan satu per satu pada satu sesi agar dapat diverifikasi.
func (d *CDataDriver) AuthorizeONU(ctx context.Context, req driver.AuthorizeRequest) error {
	script := []string{
		"config",
		fmt.Sprintf("interface gpon %s", req.PONPort),
		fmt.Sprintf("ont %d", req.Index),
		fmt.Sprintf("sn-auth %s", req.SN),
		"commit",
		"exit",
		"exit",
	}
	if _, err := d.client.ExecuteScript(ctx, script); err != nil {
		return fmt.Errorf("otorisasi ONT %s gagal: %w", req.SN, err)
	}
	return nil
}

// ExecuteRawCLI mengeksekusi perintah CLI mentah.
func (d *CDataDriver) ExecuteRawCLI(ctx context.Context, cmd string) (string, error) {
	return d.client.Execute(ctx, cmd)
}

// toONURecord memetakan ONTInfo ke model Fiber Doctor.
func (d *CDataDriver) toONURecord(ont ONTInfo) driver.ONURecord {
	rec := driver.ONURecord{
		OLT:   d.cfg.Name,
		ONUID: fmt.Sprintf("%d/%d/%d:%d", ont.Frame, ont.Slot, ont.Port, ont.ONTID),
		PON: driver.PONLocation{
			Slot:  ont.Slot,
			Port:  ont.Port,
			Index: ont.ONTID,
		},
		CustomerName: ont.Description,
		Description:  ont.Description,
		SN:           ont.SN,
		Status:       ont.Status(),
		ActualType:   ont.LineProfileName,
		LastUpdated:  time.Now(),
	}

	// Status optik: redaman < -24 dBm pada ONT berarti redaman链路 di luar
	// gold standard referensi.
	if ont.RxPower != nil {
		rec.RxOLT = ont.RxPower
	}
	if ont.OLTRxOntPower != nil {
		rec.RxONU = ont.OLTRxOntPower
	}

	return rec
}

// reAutofindSN mem-parsing baris hasil `show ont autofind`.
var reAutofindSN = regexp.MustCompile(`(?i)\b([A-Z]{3,6}[0-9A-F]{6,14})\b`)

// parseAutofindSNs mengekstrak serial number dari output autofind.
func parseAutofindSNs(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range reAutofindSN.FindAllStringSubmatch(raw, -1) {
		sn := strings.ToUpper(m[1])
		if seen[sn] {
			continue
		}
		seen[sn] = true
		out = append(out, sn)
	}
	return out
}

// vendorPrefix menebak prefix vendor dari SN (mis. "HWTC" -> Huawei).
func vendorPrefix(sn string) string {
	if len(sn) < 4 {
		return ""
	}
	return strings.ToUpper(sn[:4])
}

// formatFirst memformat perintah pertama pada rantai dengan argumen varargs.
//
// Placeholder "%s" diisi berurutan; bila perintah tidak punya placeholder,
// argumen ditambahkan sebagai suffix.
func formatFirst(commands []string, args ...string) string {
	if len(commands) == 0 {
		return ""
	}
	cmd := commands[0]
	for _, a := range args {
		if strings.Contains(cmd, "%s") {
			cmd = strings.Replace(cmd, "%s", a, 1)
			continue
		}
		cmd += " " + a
	}
	return cmd
}

// firstMeaningfulLine mengambil baris pertama yang bukan separator/prompt.
func firstMeaningfulLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || isTableSeparator(t) {
			continue
		}
		if strings.HasPrefix(t, "OLT(") || strings.HasPrefix(t, "OLT#") {
			continue
		}
		return t
	}
	return ""
}
