package zte

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gas-gatrapurna/fiberdoctor/pkg/driver"
)

// =============================================================================
// PARSER GROUND-TRUTH OLT ZTE C320
//
// Seluruh parser di file ini diverifikasi terhadap golden dump live
// OLT-SYNTH-A (C320 Version V2.1.0) yang di-capture pada 2026-11-05
// dan disimpan di testdata/zte_c320_synth/olt-synth-a/.
//
// PENTING: output `show gpon onu state` pada perangkat NYATA TIDAK memuat
// kolom Serial Number (verifikasi: tabel state tidak punya kolom SN). Pemetaan
// SN <-> ONU index hanya tersedia di `show running-config` (blok
// `interface gpon-olt_X/Y/Z` -> `onu <idx> type <T> sn <SN>`).
// =============================================================================

// reStateLine mem-parsing satu baris tabel `show gpon onu state`:
//
//	1/2/1:1     enable       enable      working      1(GPON)
//
// Kolom: OnuIndex | Admin State | OMCC State | Phase State | Channel
var reStateLine = regexp.MustCompile(`^(\d+/\d+/\d+):(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)`)

// reONURunningSN mem-parsing baris registrasi ONU di dalam blok gpon-olt:
//
//	onu 1 type ALL sn ZTEGSYNTH000002
var reONURunningSN = regexp.MustCompile(`^\s+onu\s+(\d+)\s+type\s+(\S+)\s+sn\s+(\S+)\s*$`)

// reGponOltHeader mem-parsing header blok port PON.
var reGponOltHeader = regexp.MustCompile(`^interface\s+gpon-olt_(\d+/\d+/\d+)\s*$`)

// reGponOnuHeader mem-parsing header interface ONU.
var reGponOnuHeader = regexp.MustCompile(`^interface\s+gpon-onu_(\d+/\d+/\d+):(\d+)\s*$`)

// reTContProfile mem-parsing baris profil T-CONT.
var reTContProfile = regexp.MustCompile(`^\s+tcont\s+\d+\s+(?:name\s+\S+\s+)?profile\s+(\S+)`)

// reServicePort mem-parsing baris service-port (VLAN pelanggan).
var reServicePort = regexp.MustCompile(`^\s+service-port\s+\d+\s+vport\s+\d+\s+user-vlan\s+(\d+)\s+vlan\s+(\d+)`)

// reName mem-parsing baris `name` (indeks 2 spasi = level interface).
var reName = regexp.MustCompile(`^ {2}name\s+(.+?)\s*$`)

// reOpticalValue mem-parsing nilai optik seperti "10.496(dbm)" atau "no signal".
var reOpticalValue = regexp.MustCompile(`(-?\d+(?:\.\d+)?)\s*\(dbm\)`)

// ParseZTEONUState mem-parsing output CLI `show gpon onu state`.
//
// Format aktual (live, 2026-11-05):
//
//	OnuIndex   Admin State  OMCC State  Phase State  Channel
//	--------------------------------------------------------------
//	1/2/1:1     enable       enable      working      1(GPON)
//	1/2/1:4     enable       disable     OffLine      1(GPON)
//	1/2/1:7     enable       disable     LOS          1(GPON)
//	1/2/1:33    enable       disable     DyingGasp    1(GPON)
//
// Serial Number TIDAK tersedia pada tabel ini dan sengaja dikosongkan;
// gunakan ParseZTESNIndex + ParseZTERunningConfig untuk menghimpunnya.
func ParseZTEONUState(rawOutput, oltName string) ([]driver.ONURecord, error) {
	var records []driver.ONURecord

	for _, line := range strings.Split(rawOutput, "\n") {
		line = cleanLine(line)
		if !strings.HasPrefix(strings.TrimSpace(line), "1/") && !strings.HasPrefix(strings.TrimSpace(line), "0/") {
			continue
		}

		m := reStateLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}

		slot, _ := strconv.Atoi(strings.Split(m[1], "/")[0])
		subslot, _ := strconv.Atoi(strings.Split(m[1], "/")[1])
		port, _ := strconv.Atoi(strings.Split(m[1], "/")[2])
		index, _ := strconv.Atoi(m[2])

		adminState := m[3]
		omccState := m[4]
		phaseState := m[5]
		channel := m[6]

		records = append(records, driver.ONURecord{
			OLT:        oltName,
			ONUID:      m[1] + ":" + m[2],
			AdminState: adminState,
			OMCCState:  omccState,
			PhaseState: phaseState,
			Channel:    channel,
			PON: driver.PONLocation{
				Slot:    slot,
				Subslot: subslot,
				Port:    port,
				Index:   index,
			},
			Status:      MapZTEPhaseState(phaseState),
			LastUpdated: time.Now(),
		})
	}

	return records, nil
}

// MapZTEPhaseState memetakan nilai Phase State ZTE ke status kanonik
// Fiber Doctor (Online / LOS / DyingGasp / Offline).
func MapZTEPhaseState(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "working", "ready", "online":
		return "Online"
	case "los", "losi":
		return "LOS"
	case "dyinggasp", "dying-gasp":
		return "DyingGasp"
	case "offline", "off-line":
		return "Offline"
	default:
		return "Offline"
	}
}

// ONURegistryEntry memuat data registrasi ONU pada port PON.
type ONURegistryEntry struct {
	SN         string
	DeviceType string
}

// ParseZTERegistry mengekstrak peta registrasi ONU dari `show running-config`.
//
// Kunci keluaran BERBENTUK "1/2/15:10" (slot/subslot/port:index).
//
//	interface gpon-olt_1/2/15
//	  onu 1 type V2801RGW sn ZTEGSYNTH000002
func ParseZTERegistry(rawConfig string) map[string]ONURegistryEntry {
	result := make(map[string]ONURegistryEntry)
	currentPort := ""

	for _, line := range strings.Split(rawConfig, "\n") {
		line = cleanLine(line)

		if m := reGponOltHeader.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			currentPort = m[1]
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "interface ") {
			// Blok interface lain (mis. gpon-onu_*) mengakhiri konteks port.
			currentPort = ""
			continue
		}
		if currentPort == "" {
			continue
		}

		if m := reONURunningSN.FindStringSubmatch(line); m != nil {
			result[currentPort+":"+m[1]] = ONURegistryEntry{SN: m[3], DeviceType: m[2]}
		}
	}

	return result
}

// ParseZTESNIndex mengekstrak peta "port/index -> Serial Number" dari
// `show running-config`.
//
// Kunci keluaran BERBENTUK "1/2/15:10" (slot/subslot/port:index).
func ParseZTESNIndex(rawConfig string) map[string]string {
	registry := ParseZTERegistry(rawConfig)
	result := make(map[string]string, len(registry))
	for key, entry := range registry {
		result[key] = entry.SN
	}
	return result
}

// OpticalReading adalah pembacaan daya optik satu ONU dari bulk command.
type OpticalReading struct {
	OnuID     string   // "1/2/1:1"
	DBm       *float64 // nilai dBm; nil bila "no signal" / "N/A"
	HasSignal bool
	Raw       string // token mentah dari perangkat
	Kind      string // "olt-rx" | "onu-rx" | "onu-tx"
}

// reOpticalBulkLine mem-parsing satu baris bulk optical power:
//
//	gpon-onu_1/2/1:1    -20.428(dbm)
//	gpon-onu_1/2/1:4    no signal
var reOpticalBulkLine = regexp.MustCompile(`^gpon-onu_(\d+/\d+/\d+:\d+)\s+(.*?)\s*$`)

// ParseZTEOpticalPowerBulk mem-parsing output bulk:
//
//	show pon power olt-rx gpon-olt_1/2/1   -> daya yang diterima OLT dari ONU
//	show pon power onu-rx gpon-olt_1/2/1   -> daya yang diterima ONU dari OLT
//	show pon power onu-tx gpon-olt_1/2/1   -> daya yang dikirim ONU ke OLT
//
// Format keluaran:
//
//	Onu                 Rx power
//	------------------------------------
//	gpon-onu_1/2/1:1    -20.428(dbm)
//	gpon-onu_1/2/1:4    no signal
//
// kind hanya membawa semantik kolom dan disimpan pada hasil parse.
func ParseZTEOpticalPowerBulk(raw, kind string) map[string]OpticalReading {
	result := make(map[string]OpticalReading)

	for _, line := range strings.Split(raw, "\n") {
		line = cleanLine(line)
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "gpon-onu_") {
			continue
		}

		m := reOpticalBulkLine.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}

		reading := OpticalReading{OnuID: m[1], Raw: strings.TrimSpace(m[2]), Kind: kind}
		if v, err := strconv.ParseFloat(strings.TrimSuffix(reading.Raw, "(dbm)"), 64); err == nil {
			reading.DBm = &v
			reading.HasSignal = true
		}
		result[reading.OnuID] = reading
	}

	return result
}

// ONUOptical adalah gabungan pembacaan daya optik satu ONU dari beberapa arah.
//
// OLTRx adalah nilai yang dipakai untuk klasifikasi SOP redaman referensi
// (standar emas -18 s/d -24 dBm).
type ONUOptical = driver.ONUOptical

// ParseZTEPONPorts mengekstrak daftar port PON aktif dari output
// `show gpon onu state` (unik, terurut).
func ParseZTEPONPorts(stateRaw string) []string {
	seen := make(map[string]bool)
	var ports []string

	for _, line := range strings.Split(stateRaw, "\n") {
		line = cleanLine(line)
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "1/") && !strings.HasPrefix(trimmed, "0/") {
			continue
		}
		m := reStateLine.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		port := m[1] // slot/subslot/port
		if !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}

	sort.Strings(ports)
	return ports
}

// ParseZTERunningConfig mem-parsing blok `interface gpon-onu_X/Y/Z:N` dari
// `show running-config` menjadi daftar ONU lengkap (nama, profil T-CONT,
// VLAN layanan).
//
// Contoh blok:
//
//	interface gpon-onu_1/2/15:10
//	  name demo-subscriber-01
//	  sn-bind enable sn
//	  tcont 1 profile SYNTH-1G-UP
//	  gemport 1 tcont 1 queue 1
//	  gemport 1 traffic-limit downstream SYNTH-1G-DOWN
//	  service-port 1 vport 1 user-vlan 201 vlan 201
func ParseZTERunningConfig(rawConfig, oltName string) []driver.ONURecord {
	var records []driver.ONURecord

	var cur *driver.ONURecord
	flush := func() {
		if cur != nil {
			records = append(records, *cur)
			cur = nil
		}
	}

	for _, line := range strings.Split(rawConfig, "\n") {
		line = cleanLine(line)
		trimmed := strings.TrimSpace(line)

		if m := reGponOnuHeader.FindStringSubmatch(trimmed); m != nil {
			flush()
			slot, _ := strconv.Atoi(strings.Split(m[1], "/")[0])
			subslot, _ := strconv.Atoi(strings.Split(m[1], "/")[1])
			port, _ := strconv.Atoi(strings.Split(m[1], "/")[2])
			index, _ := strconv.Atoi(m[2])

			cur = &driver.ONURecord{
				OLT:   oltName,
				ONUID: m[1] + ":" + m[2],
				PON: driver.PONLocation{
					Slot:    slot,
					Subslot: subslot,
					Port:    port,
					Index:   index,
				},
				LastUpdated: time.Now(),
			}
			continue
		}

		if cur == nil {
			continue
		}

		if m := reName.FindStringSubmatch(line); m != nil {
			cur.CustomerName = m[1]
			continue
		}
		if m := reTContProfile.FindStringSubmatch(line); m != nil {
			cur.TContProfile = m[1]
			continue
		}
		if m := reServicePort.FindStringSubmatch(line); m != nil {
			vlan, err := strconv.Atoi(m[2])
			if err == nil && !containsInt(cur.ServiceVLANs, vlan) {
				cur.ServiceVLANs = append(cur.ServiceVLANs, vlan)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "description ") {
			cur.Description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description "))
			continue
		}
	}

	flush()
	return records
}

// ParseZTEDetailInfo mem-parsing output `show gpon onu detail-info gpon-onu_X/Y/Z:N`.
//
// Contoh (live):
//
//	ONU interface:         gpon-onu_1/2/15:10
//	  Name:                demo-subscriber-01
//	  Type:                V2801RGW
//	  Serial number:       ZTEGSYNTH000001
//	  Phase state:         LOS
//	  ONU Distance:        6982m
func ParseZTEDetailInfo(raw, oltName string) (driver.ONURecord, error) {
	rec := driver.ONURecord{OLT: oltName, LastUpdated: time.Now()}

	for _, line := range strings.Split(raw, "\n") {
		line = cleanLine(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "-") {
			continue
		}

		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])

		switch strings.ToLower(key) {
		case "onu interface":
			if m := regexp.MustCompile(`gpon-onu_(\d+/\d+/\d+):(\d+)`).FindStringSubmatch(val); m != nil {
				rec.ONUID = m[1] + ":" + m[2]
				slot, _ := strconv.Atoi(strings.Split(m[1], "/")[0])
				subslot, _ := strconv.Atoi(strings.Split(m[1], "/")[1])
				port, _ := strconv.Atoi(strings.Split(m[1], "/")[2])
				index, _ := strconv.Atoi(m[2])
				rec.PON = driver.PONLocation{Slot: slot, Subslot: subslot, Port: port, Index: index}
			}
		case "name":
			rec.CustomerName = val
		case "type":
			rec.DeviceType = val
		case "serial number":
			rec.SN = val
		case "description":
			rec.Description = val
		case "admin state":
			rec.AdminState = val
		case "omcc state":
			rec.OMCCState = val
		case "phase state":
			rec.PhaseState = val
			rec.Status = MapZTEPhaseState(val)
		case "state":
			rec.ONUState = val
		case "onu distance":
			if v, err := strconv.Atoi(strings.TrimSuffix(val, "m")); err == nil {
				rec.DistanceM = v
			}
		}
	}

	if rec.ONUID == "" {
		return rec, fmt.Errorf("detail-info tidak memuat interface ONU yang valid")
	}
	return rec, nil
}

// OpticalPower adalah hasil baca `show pon power attenuation`.
//
// Format live:
//
//	           OLT                  ONU              Attenuation
//	up      Rx :no signal         Tx:N/A               N/A
//	down    Tx :10.496(dbm)       Rx:N/A               N/A
type OpticalPower struct {
	OLTRxDBm    *float64
	OLTTxDBm    *float64
	ONURxDBm    *float64
	ONUTxDBm    *float64
	Attenuation *float64
	HasSignal   bool
}

// ParseZTEOpticalPower mem-parsing output `show pon power attenuation gpon-onu_X/Y/Z:N`.
func ParseZTEOpticalPower(raw string) OpticalPower {
	var out OpticalPower

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, "up") && !strings.HasPrefix(lower, "down") {
			continue
		}
		up := strings.HasPrefix(lower, "up")

		rx, rxFound := extractOptical(trimmed, "rx")
		tx, txFound := extractOptical(trimmed, "tx")

		if up {
			// Baris "up": OLT menerima (Rx) dari ONU, ONU mengirim (Tx).
			if rxFound {
				out.OLTRxDBm = rx
			}
			if txFound {
				out.ONUTxDBm = tx
			}
		} else {
			// Baris "down": OLT mengirim (Tx), ONU menerima (Rx).
			if txFound {
				out.OLTTxDBm = tx
			}
			if rxFound {
				out.ONURxDBm = rx
			}
		}
	}

	out.HasSignal = out.OLTRxDBm != nil
	return out
}

// extractOptical mencari nilai "Rx :<n>(dbm)" atau "Tx:<n>(dbm)".
// Nilai "no signal" / "N/A" dianggap tidak tersedia.
func extractOptical(line, kind string) (*float64, bool) {
	idx := strings.Index(strings.ToLower(line), kind+" ")
	if idx < 0 {
		return nil, false
	}
	rest := line[idx+len(kind)+1:]

	colon := strings.Index(rest, ":")
	if colon < 0 {
		return nil, false
	}
	value := strings.TrimSpace(rest[colon+1:])

	m := reOpticalValue.FindStringSubmatch(value)
	if m == nil {
		return nil, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// SystemInfo memuat identitas sistem OLT dari `show system-group`.
type SystemInfo struct {
	Description  string
	Model        string
	Firmware     string
	ObjectID     string
	Uptime       string
	Contact      string
	SystemName   string
	Location     string
	SystemInfo   string
	ServiceCount int
}

var (
	reSystemModelVer = regexp.MustCompile(`(\S+)\s+Version\s+(\S+)\s+Software`)
	reServiceCount   = regexp.MustCompile(`set of\s+(\d+)\s+services`)
)

// ParseZTESystemGroup mem-parsing output `show system-group`.
func ParseZTESystemGroup(raw string) SystemInfo {
	info := SystemInfo{}

	for _, line := range strings.Split(raw, "\n") {
		line = cleanLine(line)
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		val := strings.TrimSpace(strings.Trim(line[idx+1:], "\x00\r "))

		switch key {
		case "system description":
			info.Description = val
			if m := reSystemModelVer.FindStringSubmatch(val); m != nil {
				info.Model = m[1]
				info.Firmware = m[2]
			}
		case "system objectid":
			info.ObjectID = val
		case "started before":
			info.Uptime = val
		case "contact with":
			info.Contact = val
		case "system name":
			info.SystemName = val
		case "location":
			info.Location = val
		case "system info":
			info.SystemInfo = val
		}
	}

	if m := reServiceCount.FindStringSubmatch(raw); m != nil {
		info.ServiceCount, _ = strconv.Atoi(m[1])
	}
	return info
}

// CardInfo memuat informasi satu kartu OLT dari `show card`.
type CardInfo struct {
	Rack     int
	Shelf    int
	Slot     int
	CfgType  string
	RealType string
	Ports    int
	HardVer  string
	SoftVer  string
	Status   string
}

// ParseZTECard mem-parsing output `show card`.
//
//	header: Rack Shelf Slot CfgType RealType Port  HardVer SoftVer         Status
//	baris : 1    1     2    GTGH    GTGHG    16    V1.0.0  V2.1.0          INSERVICE
func ParseZTECard(raw string) []CardInfo {
	var cards []CardInfo

	lines := strings.Split(raw, "\n")
	offsets := headerOffsets("")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Rack Shelf Slot") {
			offsets = headerOffsets(line)
			continue
		}
		if offsets == nil {
			continue
		}
		fields := sliceRow(line, offsets)
		if len(fields) < 4 {
			continue
		}
		rack, _ := strconv.Atoi(fields[0])
		shelf, _ := strconv.Atoi(fields[1])
		slot, _ := strconv.Atoi(fields[2])
		if rack == 0 && shelf == 0 && slot == 0 {
			continue
		}

		card := CardInfo{Rack: rack, Shelf: shelf, Slot: slot, CfgType: fields[3]}
		if len(fields) > 4 {
			card.RealType = fields[4]
		}
		if len(fields) > 5 {
			card.Ports, _ = strconv.Atoi(fields[5])
		}
		if len(fields) > 6 {
			card.HardVer = fields[6]
		}
		if len(fields) > 7 {
			card.SoftVer = fields[7]
		}
		if len(fields) > 8 {
			card.Status = fields[8]
		}
		cards = append(cards, card)
	}

	return cards
}

// ParseZTECardTemperature mem-parsing output `show card-temperature`.
//
//	header: Rack Shelf Slot Temperature Temperature(5m) Temperature(1h) Optical-Temp
//	baris : 1    1     2    44          44              44              N/A.
//
// Kunci peta: "rack/shelf/slot" -> suhu (derajat Celsius).
func ParseZTECardTemperature(raw string) map[string]float64 {
	result := make(map[string]float64)

	lines := strings.Split(raw, "\n")
	var offsets []int
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Rack Shelf Slot") {
			offsets = headerOffsets(line)
			continue
		}
		if offsets == nil {
			continue
		}
		fields := sliceRow(line, offsets)
		if len(fields) < 4 {
			continue
		}
		temp, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue // N/A.
		}
		rack, _ := strconv.Atoi(fields[0])
		shelf, _ := strconv.Atoi(fields[1])
		slot, _ := strconv.Atoi(fields[2])
		result[fmt.Sprintf("%d/%d/%d", rack, shelf, slot)] = temp
	}

	return result
}

// ParseZTEProfileTCONT mem-parsing profil T-CONT dari `show gpon profile tcont`.
//
//	profile tcont SYNTH-1G-UP type 5 fixed 64 assured 64 maximum 1048064
func ParseZTEProfileTCONT(raw string) map[string]string {
	re := regexp.MustCompile(`profile\s+tcont\s+(\S+)\s+type\s+(\d+)\s+fixed\s+(\d+)\s+assured\s+(\d+)\s+maximum\s+(\d+)`)
	result := make(map[string]string)
	for _, m := range re.FindAllStringSubmatch(raw, -1) {
		result[m[1]] = fmt.Sprintf("type=%s fixed=%s assured=%s maximum=%s", m[2], m[3], m[4], m[5])
	}
	return result
}

// headerOffsets menghitung posisi awal setiap kolom pada baris header tabel CLI.
func headerOffsets(header string) []int {
	if strings.TrimSpace(header) == "" {
		return nil
	}
	var offsets []int
	pos := 0
	for _, field := range strings.Fields(header) {
		idx := strings.Index(header[pos:], field)
		if idx < 0 {
			continue
		}
		start := pos + idx
		offsets = append(offsets, start)
		pos = start + len(field)
	}
	return offsets
}

// sliceRow memotong baris data mengikuti offset kolom header.
func sliceRow(line string, offsets []int) []string {
	out := make([]string, len(offsets))
	for i, off := range offsets {
		end := len(line)
		if i+1 < len(offsets) {
			end = offsets[i+1]
		}
		if off > len(line) {
			off = len(line)
		}
		if end > len(line) {
			end = len(line)
		}
		if end < off {
			end = off
		}
		out[i] = strings.TrimSpace(line[off:end])
	}
	return out
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// cleanLine membersihkan satu baris output CLI: membuang byte NUL (0x00)
// yang disisipkan firmware ZTE di akhir beberapa blok output, serta CR.
func cleanLine(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimRight(s, "\r")
}
