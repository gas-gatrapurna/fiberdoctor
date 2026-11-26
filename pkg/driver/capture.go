package driver

import "context"

// CaptureKind menentukan kapan sebuah langkah capture dieksekusi.
type CaptureKind int

const (
	// CaptureStatic adalah perintah read-only tetap.
	CaptureStatic CaptureKind = iota
	// CapturePerPort adalah perintah yang dijalankan sekali per port PON.
	CapturePerPort
	// CapturePerSample adalah perintah yang memerlukan satu ONU/ONT valid
	// sebagai argumen (mis. `show ont optical-info <port> <ont>`).
	CapturePerSample
)

// CapturePlanner merancang langkah-langkah golden dump untuk sebuah vendor.
//
// Tujuannya: `cmd/fd_golden` menjadi alat generik yang bisa men-dump OLT
// ZTE maupun C-Data tanpa logika per-vendor di dalamnya.
//
// Aturan penting: HANYA perintah read-only yang boleh masuk rencana.
type CapturePlanner interface {
	// StaticCommands mengembalikan daftar perintah tetap.
	StaticCommands() []string

	// Ports mengembalikan daftar port PON aktif (boleh kosong bila vendor
	// tidak mendukung inventaris per port).
	Ports(ctx context.Context) ([]string, error)

	// SampleID mengembalikan ID ONU/ONT contoh yang valid, mis. "1/2/15:10"
	// (ZTE) atau "2/3" (C-Data). Boleh kosong bila tidak ada.
	SampleID(ctx context.Context) (string, error)

	// PortCommands mengembalikan perintah yang dijalankan per port PON.
	PortCommands(port string) []string

	// PortSubdir adalah nama subfolder untuk menyimpan keluaran per-port.
	PortSubdir() string

	// SampleCommands mengembalikan perintah yang dijalankan untuk satu
	// ONU/ONT contoh.
	SampleCommands(sample string) []string
}
