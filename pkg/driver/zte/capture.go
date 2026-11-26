package zte

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
)

// staticCommands adalah perintah read-only tetap untuk golden dump ZTE.
//
// Seluruh nama perintah sudah DIVERIFIKASI terhadap pohon `show ?` live di
// OLT-SYNTH-A & OLT-SYNTH-B (C320 V2.1.0, 2026-11-05).
var staticCommands = []string{
	"show version-running",
	"show system-group",
	"show card",
	"show card-temperature",
	"show gpon onu state",
	"show gpon onu uncfg",
	"show gpon profile",
	"show onu-type",
	"show running-config",
}

// StaticCommands mengembalikan daftar perintah tetap ZTE.
func (d *ZTEC320Driver) StaticCommands() []string {
	out := make([]string, len(staticCommands))
	copy(out, staticCommands)
	return out
}

// Ports mengembalikan daftar port PON aktif dari tabel state.
func (d *ZTEC320Driver) Ports(ctx context.Context) ([]string, error) {
	out, err := d.telnet.ExecuteCommand(ctx, "show gpon onu state")
	if err != nil {
		return nil, fmt.Errorf("gagal membaca state ONU: %w", err)
	}
	return ParseZTEPONPorts(out), nil
}

// SampleID mengembalikan ONU pertama yang terdaftar, mis. "1/2/15:10".
func (d *ZTEC320Driver) SampleID(ctx context.Context) (string, error) {
	out, err := d.telnet.ExecuteCommand(ctx, "show gpon onu state")
	if err != nil {
		return "", fmt.Errorf("gagal membaca state ONU: %w", err)
	}
	records, err := ParseZTEONUState(out, d.cfg.Name)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", fmt.Errorf("tidak ada ONU terdaftar pada %s", d.cfg.Name)
	}
	return records[0].ONUID, nil
}

// PortCommands mengembalikan perintah redaman optik BULK untuk satu port.
//
// Dua perintah per port (OLT-Rx + ONU-Rx) menggantikan pendekatan 1 perintah
// per ONU: 944 ONU cukup 72 panggilan, bukan 944.
func (d *ZTEC320Driver) PortCommands(port string) []string {
	return []string{
		"show pon power olt-rx gpon-olt_" + port,
		"show pon power onu-rx gpon-olt_" + port,
	}
}

// PortSubdir mengembalikan nama subfolder keluaran per-port.
func (d *ZTEC320Driver) PortSubdir() string { return "optical" }

// SampleCommands mengembalikan perintah detail untuk satu ONU.
func (d *ZTEC320Driver) SampleCommands(sample string) []string {
	iface := "gpon-onu_" + sample
	return []string{
		"show gpon onu detail-info " + iface,
		"show pon power attenuation " + iface,
		"show running-config interface " + iface,
	}
}

// ParseZTESamplePorts mem-parse ID ONU "1/2/15:10" menjadi port PON "1/2/15".
func ParseZTESamplePorts(sample string) string {
	idx := strings.Index(sample, ":")
	if idx <= 0 {
		return sample
	}
	return sample[:idx]
}

// ZTETSampleIndex mengembalikan indeks ONU dari ID "1/2/15:10".
func ZTETSampleIndex(sample string) int {
	idx := strings.Index(sample, ":")
	if idx < 0 {
		return 0
	}
	n, _ := strconv.Atoi(sample[idx+1:])
	return n
}

// compile-time check: driver ZTE memenuhi kontrak CapturePlanner.
var _ driver.CapturePlanner = (*ZTEC320Driver)(nil)
