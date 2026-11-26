// fd_golden — alat pengambilan "golden dump" CLI OLT untuk validasi parser Go.
//
// Roadmap F0: ambil ground-truth output CLI OLT ZTE C320 (SYNTH-A) sehingga
// pkg/driver/zte dapat divalidasi terhadap realita perangkat, bukan asumsi.
//
// Keamanan (SEC-03):
//   - Hanya perintah berawalan "show " yang boleh dikirim. Perintah yang
//     mengubah state (configure/write/reboot/reset/delete) ditolak BEFORE
//     menyentuh perangkat.
//   - Kredensial TIDAK pernah ditulis ke disk oleh tool ini dan TIDAK
//     di-hardcode di source. Sumbernya: environment variable FD_OLT_PASS,
//     atau file vault SOP yang diparse saat runtime.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
	_ "github.com/gas-gatrapurna/fiberdoctor/pkg/driver/cdata" // mendaftarkan factory cdata_*
	_ "github.com/gas-gatrapurna/fiberdoctor/pkg/driver/zte"   // mendaftarkan factory zte_*
)

// allowedPrefix adalah satu-satunya bentuk perintah yang boleh dieksekusi.
const allowedPrefix = "show "

// defaultCommands adalah battery perintah read-only untuk golden dump.
// Seluruh nama perintah sudah DIVERIFIKASI terhadap pohon `show ?` live
// di OLT-SYNTH-A (C320 V2.1.0) pada 2026-11-05.
var defaultCommands = []string{
	"show version-running",
	"show system-group",
	"show card",
	"show card-temperature",
	"show card-power",
	"show rack",
	"show shelf",
	"show gpon ?",
	"show gpon global",
	"show gpon olt",
	"show gpon slot",
	"show gpon onu state",
	"show gpon onu uncfg",
	"show gpon profile",
	"show onu-type",
	"show running-config",
}

// sampleCommands adalah perintah yang memerlukan satu ONU valid sebagai argumen.
// ONU sampel dipilih OTOMATIS dari output `show gpon onu state`, sehingga tool
// yang sama bisa dipakai untuk OLT mana pun tanpa konfigurasi ulang.
func sampleCommands(onuID string) []string {
	iface := "gpon-onu_" + onuID
	return []string{
		"show gpon onu detail-info " + iface,
		"show pon power attenuation " + iface,
		"show running-config interface " + iface,
	}
}

// reFirstONU mengambil OnuIndex pertama dari tabel `show gpon onu state`.
var reFirstONU = regexp.MustCompile(`(?m)^(\d+/\d+/\d+:\d+)\s`)

var (
	host    = flag.String("host", "", "IP OLT target (wajib diisi)")
	name    = flag.String("name", "", "Nama OLT (wajib diisi; dipakai di manifest & default outdir)")
	vendor  = flag.String("vendor", "zte_c320", "Kunci vendor driver (zte_c320, zte_c300, cdata_fd1602s-b1, ...)")
	model   = flag.String("model", "C320", "Model perangkat (untuk resolusi profil varian)")
	port    = flag.Int("port", 23, "Port Telnet OLT")
	user    = flag.String("user", "", "User Telnet OLT (wajib diisi)")
	vault   = flag.String("vault", "", "File vault untuk parsing kredensial (opsional; default: env FD_CRED_VAULT)")
	outDir  = flag.String("outdir", "", "Direktori output golden dump (default: <repo>/data/golden/<olt>-<tanggal>)")
	timeout = flag.Int("timeout", 25, "Timeout per perintah (detik)")
	only    = flag.String("only", "", "Daftar perintah dipisah koma (override default)")
)

type manifestEntry struct {
	Command   string `json:"command"`
	File      string `json:"file"`
	Bytes     int    `json:"bytes"`
	Lines     int    `json:"lines"`
	DurationS string `json:"duration"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

type manifest struct {
	OLT        string          `json:"olt"`
	Host       string          `json:"host"`
	Port       int             `json:"port"`
	Vendor     string          `json:"vendor"`
	Model      string          `json:"model"`
	Generated  string          `json:"generated_at"`
	Mode       string          `json:"mode"`
	ReadOnly   bool            `json:"read_only_guard"`
	Commands   []manifestEntry `json:"commands"`
	TotalBytes int             `json:"total_bytes"`
}

func main() {
	flag.Parse()

	if *host == "" || *name == "" || *user == "" {
		fatal("--host, --name, dan --user wajib diisi")
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

	cmds := defaultCommands
	if strings.TrimSpace(*only) != "" {
		cmds = nil
		for _, c := range strings.Split(*only, ",") {
			if c = strings.TrimSpace(c); c != "" {
				cmds = append(cmds, c)
			}
		}
	}

	out := *outDir
	if out == "" {
		wd, err := os.Getwd()
		if err != nil {
			fatal("gagal menentukan working directory: %v", err)
		}
		stamp := time.Now().Format("2006-01-02")
		out = filepath.Join(wd, "data", "golden", fmt.Sprintf("olt-%s-%s", slugify(*name), stamp))
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fatal("gagal membuat output dir: %v", err)
	}

	cfg := driver.OLTConfig{
		ID:             *name,
		Name:           *name,
		Vendor:         *vendor,
		Model:          *model,
		Host:           *host,
		TelnetPort:     *port,
		TelnetUser:     *user,
		TelnetPass:     pass,
		TimeoutSeconds: *timeout,
	}

	d, err := driver.GetDriver(cfg)
	if err != nil {
		fatal("gagal membuat driver: %v", err)
	}

	// Rencana capture per-vendor. Bila driver tidak mengimplementasikan
	// CapturePlanner, jatuh ke daftar perintah statis bawaan.
	planner, hasPlanner := d.(driver.CapturePlanner)
	if hasPlanner && strings.TrimSpace(*only) == "" {
		cmds = planner.StaticCommands()
	}

	mf := manifest{
		OLT:       cfg.Name,
		Host:      cfg.Host,
		Port:      cfg.TelnetPort,
		Vendor:    "ZTE",
		Model:     cfg.Model,
		Generated: time.Now().Format(time.RFC3339),
		Mode:      "read-only CLI capture (show *)",
		ReadOnly:  true,
	}

	// Guard read-only: tolak perintah berbahaya sebelum runtime.
	// Guard ini berlaku untuk SEMUA perintah — statis maupun hasil rencana
	// per-vendor.
	guard := func(list []string) {
		for _, c := range list {
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c)), allowedPrefix) {
				fatal("perintah ditolak oleh guard read-only: %q (hanya %q yang diizinkan)", c, allowedPrefix)
			}
		}
	}
	guard(cmds)

	fmt.Printf("=== FIBER DOCTOR · GOLDEN DUMP (READ-ONLY) ===\n")
	fmt.Printf("Target : %s (%s:%d)\n", cfg.Name, cfg.Host, cfg.TelnetPort)
	fmt.Printf("Vendor : %s / %s\n", cfg.Vendor, cfg.Model)
	fmt.Printf("Output : %s\n\n", out)

	// run mengeksekusi satu perintah read-only, menyimpan keluarannya, dan
	// mencatatnya ke manifest.
	run := func(cmd, subdir string) (manifestEntry, string) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)

		fmt.Printf("[RUN ] %s\n", cmd)
		res, err := d.ExecuteRawCLI(ctx, cmd)
		cancel()
		dur := time.Since(start)

		entry := manifestEntry{
			Command:   cmd,
			DurationS: dur.Round(time.Millisecond).String(),
			OK:        err == nil,
		}
		if err != nil {
			entry.Error = err.Error()
		}

		fileName := sanitize(cmd) + ".txt"
		if subdir != "" {
			fileName = filepath.Join(subdir, fileName)
			if err := os.MkdirAll(filepath.Join(out, subdir), 0o755); err != nil {
				fatal("gagal membuat subdir %s: %v", subdir, err)
			}
		}
		entry.File = filepath.ToSlash(fileName)
		entry.Bytes = len(res)
		entry.Lines = strings.Count(res, "\n") + 1
		mf.Commands = append(mf.Commands, entry)
		mf.TotalBytes += len(res)

		body := res
		if err != nil {
			body = res + "\n\n[ERROR] " + err.Error() + "\n"
		}
		if werr := os.WriteFile(filepath.Join(out, fileName), []byte(body), 0o644); werr != nil {
			fatal("gagal menulis %s: %v", fileName, werr)
		}
		status := "[ OK ]"
		if err != nil {
			status = "[FAIL]"
		}
		fmt.Printf("%s %-46s %7d B / %5d lines / %s\n", status, cmd, entry.Bytes, entry.Lines, entry.DurationS)
		return entry, res
	}

	// Jalankan perintah statis.
	for _, cmd := range cmds {
		run(cmd, "")
	}

	// Rencana per-vendor: inventaris per port PON & detail per ONU/ONT.
	if hasPlanner && strings.TrimSpace(*only) == "" {
		ctx := context.Background()

		if ports, err := planner.Ports(ctx); err == nil && len(ports) > 0 {
			fmt.Printf("[INFO] %d port PON terdeteksi; mengambil inventaris per port...\n", len(ports))
			for _, port := range ports {
				list := planner.PortCommands(port)
				guard(list)
				for _, cmd := range list {
					run(cmd, planner.PortSubdir())
				}
			}
		}

		if sample, err := planner.SampleID(ctx); err == nil && sample != "" {
			fmt.Printf("[INFO] sampel terdeteksi: %s\n", sample)
			list := planner.SampleCommands(sample)
			guard(list)
			for _, cmd := range list {
				run(cmd, "sample")
			}
		} else if err != nil {
			fmt.Printf("[WARN] sampel tidak terdeteksi: %v\n", err)
		}
	}

	mfPath := filepath.Join(out, "manifest.json")
	b, _ := json.MarshalIndent(mf, "", "  ")
	if err := os.WriteFile(mfPath, append(b, '\n'), 0o644); err != nil {
		fatal("gagal menulis manifest: %v", err)
	}

	fmt.Printf("\n=== RINGKASAN ===\n")
	fmt.Printf("Perintah   : %d\n", len(mf.Commands))
	fmt.Printf("Total data : %d byte\n", mf.TotalBytes)
	fmt.Printf("Manifest   : %s\n", mfPath)
}

// credFromVault membaca kredensial OLT dari dokumen SOP vault tanpa
// menyimpannya ke disk atau meng-hardcode di source.
func credFromVault(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// Baris contoh: "- Kredensial: `user` / `password`"
	re := regexp.MustCompile("(?i)kredensial\\s*:\\s*`([^`]+)`\\s*/\\s*`([^`]+)`")
	m := re.FindStringSubmatch(string(b))
	if m == nil {
		return "", fmt.Errorf("pola kredensial tidak ditemukan di vault")
	}
	return m[2], nil
}

// sanitize mengubah perintah menjadi nama file aman.
func sanitize(cmd string) string {
	s := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cmd)), "show ")
	s = strings.TrimSpace(s)
	r := regexp.MustCompile(`[^a-z0-9]+`)
	s = r.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = "root"
	}
	return s
}

// slugify mengubah nama OLT menjadi slug aman untuk nama direktori.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", a...)
	os.Exit(1)
}
