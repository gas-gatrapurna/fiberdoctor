// fd_doctor — NOC health-check live untuk OLT ZTE via Fiber Doctor driver.
//
// Menghubungkan Pilar 1 (OLT & ONU Core) dengan Pilar 2 (Fault Triage L1-L7):
//  1. GetSummary   -> kesehatan chassis, kartu, suhu, agregat ONU
//  2. ListONUs     -> inventaris ONU + SN + VLAN (live, bukan cache)
//  3. DiagnoseGPONTriage -> diagnosis per port PON berdasarkan blast radius
//
// Semua perintah yang dikirim adalah READ-ONLY (prefix "show").
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
	_ "github.com/gas-gatrapurna/fiberdoctor/pkg/driver/zte"
	"github.com/gas-gatrapurna/fiberdoctor/pkg/triage"
)

var (
	host    = flag.String("host", "", "IP OLT target (wajib diisi)")
	port    = flag.Int("port", 23, "Port Telnet OLT")
	user    = flag.String("user", "", "User Telnet OLT (wajib diisi)")
	vault   = flag.String("vault", "", "File vault kredensial (opsional; default: env FD_CRED_VAULT)")
	oltName = flag.String("name", "", "Nama OLT (wajib diisi)")
	timeout = flag.Int("timeout", 60, "Timeout per perintah (detik)")
)

func main() {
	flag.Parse()

	if *host == "" || *user == "" || *oltName == "" {
		fatal("--host, --user, dan --name wajib diisi")
	}

	pass := os.Getenv("FD_OLT_PASS")
	if pass == "" {
		vaultPath := *vault
		if vaultPath == "" {
			vaultPath = os.Getenv("FD_CRED_VAULT")
		}
		if vaultPath == "" {
			fatal("tidak ada sumber kredensial: set FD_OLT_PASS atau FD_CRED_VAULT")
		}
		p, err := credFromVault(vaultPath)
		if err != nil {
			fatal("gagal membaca kredensial dari vault %q: %v (atau set FD_OLT_PASS)", vaultPath, err)
		}
		pass = p
	}

	d, err := driver.GetDriver(driver.OLTConfig{
		ID:             *oltName,
		Name:           *oltName,
		Vendor:         "zte_c320",
		Model:          "C320",
		Host:           *host,
		TelnetPort:     *port,
		TelnetUser:     *user,
		TelnetPass:     pass,
		TimeoutSeconds: *timeout,
	})
	if err != nil {
		fatal("gagal membuat driver: %v", err)
	}

	ctx := context.Background()

	// ---- Pilar 1: ringkasan kesehatan OLT ----
	fmt.Println("=== FIBER DOCTOR · LIVE HEALTH CHECK ===")
	summary, err := d.GetSummary(ctx)
	if err != nil {
		fatal("gagal GetSummary: %v", err)
	}

	fmt.Printf("\n[ CHASSIS ]\n")
	fmt.Printf("  Sistem        : %s (%s %s)\n", summary.SystemName, summary.Vendor, summary.Model)
	fmt.Printf("  Firmware      : %s\n", summary.Firmware)
	fmt.Printf("  Uptime        : %s\n", summary.Uptime)
	fmt.Printf("  Lokasi        : %s\n", summary.Location)
	fmt.Printf("  Suhu maks     : %.0f C\n", summary.TemperatureC)
	fmt.Printf("  Kartu         : %d (INSERVICE %d)\n", summary.Cards, summary.CardsInService)
	fmt.Printf("  ONU           : total %d | online %d | offline %d | LOS %d | dyinggasp %d\n",
		summary.TotalONUs, summary.OnlineONUs, summary.OfflineONUs, summary.LosONUs, summary.DyingGaspONUs)

	// ---- Pilar 1: inventaris ONU ----
	fmt.Printf("\n[ INVENTARIS ]\n")
	onus, err := d.ListONUs(ctx)
	if err != nil {
		fatal("gagal ListONUs: %v", err)
	}

	withSN, withName, withVLAN := 0, 0, 0
	for _, o := range onus {
		if o.SN != "" {
			withSN++
		}
		if o.CustomerName != "" {
			withName++
		}
		if len(o.ServiceVLANs) > 0 {
			withVLAN++
		}
	}
	fmt.Printf("  ONU terbaca   : %d\n", len(onus))
	fmt.Printf("  Lengkap SN    : %d / %d\n", withSN, len(onus))
	fmt.Printf("  Nama pelanggan: %d / %d\n", withName, len(onus))
	fmt.Printf("  VLAN terkunci : %d / %d\n", withVLAN, len(onus))

	// ---- Pilar 2: triage per port PON ----
	type portStat struct {
		port      string
		total     int
		critical  int
		los       int
		dyingGasp int
		offline   int
	}
	ports := map[string]*portStat{}
	for _, o := range onus {
		key := fmt.Sprintf("%d/%d/%d", o.PON.Slot, o.PON.Subslot, o.PON.Port)
		st, ok := ports[key]
		if !ok {
			st = &portStat{port: key}
			ports[key] = st
		}
		st.total++
		if o.Status == "LOS" || o.Status == "DyingGasp" {
			st.critical++
		}
		if o.Status == "LOS" {
			st.los++
		}
		if o.Status == "DyingGasp" {
			st.dyingGasp++
		}
		if o.Status == "Offline" {
			st.offline++
		}
	}

	keys := make([]string, 0, len(ports))
	for k := range ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// ---- Pilar 2: redaman optik BULK per port PON ----
	//
	// 2 panggilan CLI per port (olt-rx + onu-rx), bukan 1 per ONU.
	// 2 panggilan CLI per port (olt-rx + onu-rx), bukan 1 per ONU.
	opticalReports := map[string]triage.PortOpticalReport{}
	bulk, bulkOK := d.(driver.OpticalBulkReader)
	if bulkOK {
		fmt.Printf("\n[ REDAMAN OPTIK BULK PER PORT ]\n")
		for _, k := range keys {
			readings, err := bulk.GetOpticalPowerBulk(ctx, k)
			if err != nil {
				fmt.Printf("  %-10s GAGAL: %v\n", k, err)
				continue
			}
			rxByOnu := make(map[string]*float64, len(readings))
			for onuID, reading := range readings {
				rxByOnu[onuID] = reading.OLTRx
			}
			opticalReports[k] = triage.AnalyzePortOptical(k, rxByOnu)
		}
	} else {
		fmt.Printf("\n[ REDAMAN OPTIK BULK ]\n  Driver tidak mendukung bulk optical power\n")
	}

	fmt.Printf("\n[ TRIAGE PER PORT PON ]\n")
	fmt.Printf("  %-10s %6s %8s %6s %8s %8s %8s %8s  %-22s\n",
		"PORT", "TOTAL", "OFFLINE", "LOS", "DYING", "NO-SIG", "CRIT-RX", "WARN-RX", "DIAGNOSIS")
	alerts := 0
	for _, k := range keys {
		st := ports[k]

		var diag triage.IncidentDiagnosis
		if report, ok := opticalReports[k]; ok {
			diag = triage.DiagnosePortOptical(*oltName, k, st.total, st.los, st.dyingGasp, report)
		} else {
			diag = triage.DiagnoseGPONTriage(*oltName, k, st.total, st.critical, st.los, 0)
		}

		noSig, critRx, warnRx := 0, 0, 0
		if report, ok := opticalReports[k]; ok {
			noSig, critRx, warnRx = report.NoSignal, report.Critical, report.Warning
		}

		flagged := ""
		if diag.Severity != triage.SeverityInfo {
			flagged = "  <== " + string(diag.Severity)
			alerts++
		}
		fmt.Printf("  %-10s %6d %8d %6d %8d %8d %8d %8d  %-22s%s\n",
			k, st.total, st.offline, st.los, st.dyingGasp, noSig, critRx, warnRx,
			diag.DiagnosisCode, flagged)
	}
	fmt.Printf("\n  Port dengan gangguan: %d dari %d\n", alerts, len(ports))

	// Rincian ONU dengan redaman kritis (siap ditindaklanjuti lapangan).
	if len(opticalReports) > 0 {
		fmt.Printf("\n[ ONU REDAMAN KRITIS (Rx <= -27 dBm) ]\n")
		found := 0
		for _, k := range keys {
			report := opticalReports[k]
			if len(report.CriticalOnus) == 0 {
				continue
			}
			found += len(report.CriticalOnus)
			fmt.Printf("  Port %-10s : %s (worst %.2f dBm)\n",
				k, strings.Join(report.CriticalOnus, ", "), *report.WorstRxDBm)
		}
		if found == 0 {
			fmt.Printf("  Tidak ada ONU dengan redaman kritis pada port manapun.\n")
		} else {
			fmt.Printf("\n  Total ONU redaman kritis: %d\n", found)
		}
	}

	// Rekap VLAN pelanggan
	vlanCount := map[int]int{}
	for _, o := range onus {
		for _, v := range o.ServiceVLANs {
			vlanCount[v]++
		}
	}
	if len(vlanCount) > 0 {
		fmt.Printf("\n[ SEBARAN VLAN ]\n")
		type vc struct {
			v int
			n int
		}
		list := make([]vc, 0, len(vlanCount))
		for v, n := range vlanCount {
			list = append(list, vc{v, n})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v < list[j].v })
		for _, item := range list {
			fmt.Printf("  VLAN %-6d %d ONU\n", item.v, item.n)
		}
	}

	fmt.Printf("\n[Selesai %s]\n", time.Now().Format(time.RFC3339))
}

// credFromVault membaca kredensial OLT dari dokumen SOP vault tanpa
// menyimpannya ke disk atau meng-hardcode di source.
func credFromVault(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile("(?i)kredensial\\s*:\\s*`([^`]+)`\\s*/\\s*`([^`]+)`")
	m := re.FindStringSubmatch(string(b))
	if m == nil {
		return "", fmt.Errorf("pola kredensial tidak ditemukan di vault")
	}
	return m[2], nil
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", a...)
	os.Exit(1)
}
