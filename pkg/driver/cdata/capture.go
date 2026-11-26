package cdata

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
)

// staticCommands adalah perintah read-only tetap untuk golden dump C-Data.
//
// Nama perintah mengikuti manual CLI resmi FD16xx v3.0 dan set perintah yang
// sudah dipakai adapter produksi.
var staticCommands = []string{
	"show service status",
	"show version",
	"show device",
	"show system information",
	"show ont register-statistics all",
}

// StaticCommands mengembalikan daftar perintah tetap C-Data.
func (d *CDataDriver) StaticCommands() []string {
	out := make([]string, len(staticCommands))
	copy(out, staticCommands)
	return out
}

// Ports mengembalikan daftar port PON yang memiliki ONT terdaftar.
func (d *CDataDriver) Ports(ctx context.Context) ([]string, error) {
	ports, err := d.ListPONPorts(ctx)
	if err != nil {
		return nil, err
	}

	// ListPONPorts mengembalikan "frame/slot/port"; untuk C-Data perintah
	// memakai port-id saja, jadi ambil bagian terakhir.
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		parts := strings.Split(p, "/")
		out = append(out, parts[len(parts)-1])
	}
	return out, nil
}

// SampleID mengembalikan satu ONT terdaftar sebagai "port/ont-id".
func (d *CDataDriver) SampleID(ctx context.Context) (string, error) {
	ports, err := d.Ports(ctx)
	if err != nil {
		return "", err
	}

	for _, port := range ports {
		out, err := d.client.Execute(ctx, formatFirst(d.cmds.ONTList, port))
		if err != nil {
			continue
		}
		summary := ParseONTInfoSummary(out)
		if len(summary.ONTs) == 0 {
			continue
		}
		return fmt.Sprintf("%d/%d", summary.ONTs[0].Port, summary.ONTs[0].ONTID), nil
	}

	return "", fmt.Errorf("tidak ada ONT terdaftar pada %s", d.cfg.Name)
}

// PortCommands mengembalikan perintah inventaris ONT untuk satu port PON.
//
// C-Data TIDAK menyediakan mode optik bulk per port seperti ZTE; daftar ONT
// memang tersedia per port (`show ont info <port> all`).
func (d *CDataDriver) PortCommands(port string) []string {
	return []string{
		"show ont info " + port + " all",
		"show ont autofind " + port + " all",
	}
}

// PortSubdir mengembalikan nama subfolder keluaran per-port.
func (d *CDataDriver) PortSubdir() string { return "ports" }

// SampleCommands mengembalikan perintah detail + optik untuk satu ONT.
func (d *CDataDriver) SampleCommands(sample string) []string {
	parts := strings.Split(sample, "/")
	if len(parts) != 2 {
		return nil
	}
	port, ontID := parts[0], parts[1]

	return []string{
		"show ont info " + port + " " + ontID,
		"show ont optical-info " + port + " " + ontID,
		"show ont postback optical-info " + port + " " + ontID,
		"show ont failed-config " + port + " " + ontID,
		"show ont port state " + port + " " + ontID + " eth all",
	}
}

// CDataSamplePort mengembalikan port-id dari sample "port/ont-id".
func CDataSamplePort(sample string) string {
	parts := strings.Split(sample, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// CDataSampleONTID mengembalikan ont-id dari sample "port/ont-id".
func CDataSampleONTID(sample string) string {
	parts := strings.Split(sample, "/")
	if len(parts) < 2 {
		return ""
	}
	n, _ := strconv.Atoi(parts[1])
	return strconv.Itoa(n)
}

// compile-time check: driver C-Data memenuhi kontrak CapturePlanner.
var _ driver.CapturePlanner = (*CDataDriver)(nil)
