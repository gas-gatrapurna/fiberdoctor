// Package cms mengimplementasikan klien C-Data CMS Northbound Interface.
//
// CMS adalah platform agregat C-Data yang menyatukan SEMUA OLT C-Data
// (varian apa pun: FD1601S/1602S/1604S/1608S/1612S/1616S, FD12xx, FD11xx,
// FD602XW, FD704G, FD804G) ke dalam satu endpoint REST. Ini complimentarity
// penting terhadap driver CLI: CLI untuk kendali per perangkat, CMS untuk
// inventaris armada lintas varian.
//
// Kontrak API (manual "CMS Northbound Interface", C-Data CMS 3.2):
//
//	Auth      : header X-Token (API key)
//	Daftar    : GET /v1/openapi/cms/devices/page?current=1&size=10&category=OLT|ONU
//	          &manufacturer=&model=&runningState=
//	PON info  : GET /v1/openapi/cms/devices/pon-info?externalId=<id>
//	WAN config: GET /v1/openapi/cms/devices/wan-confignew?externalId=<id>
//
// Semua respons memakai amplop: {"code":0,"msg":"","data":{...},"ext":null}
// dengan code != 0 berarti gagal (mis. 701004 = API Key violation).
package cms

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Client adalah klien CMS Northbound.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New membuat klien CMS.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// envelope adalah amplop respons CMS.
type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
	Ext  json.RawMessage `json:"ext"`
}

// Device adalah satu record perangkat (OLT atau ONU) dari CMS.
type Device struct {
	DeviceID        string   `json:"deviceId"`
	ExternalID      string   `json:"externalId"`
	SN              string   `json:"sn"`
	MAC             string   `json:"mac"`
	PONID           string   `json:"ponId"`
	ONUID           string   `json:"onuId"`
	DeviceDesc      string   `json:"deviceDesc"`
	DeviceName      string   `json:"deviceName"`
	Channels        []string `json:"channels"`
	RunningState    string   `json:"runningState"`
	ParentID        string   `json:"parentId"`
	OLTDisplay      string   `json:"oltDisplay"`
	OLTDeviceType   string   `json:"oltDeviceType"`
	OLTRunningState string   `json:"oltRunningState"`
	Deactive        bool     `json:"deactive"`
	CustomerName    string   `json:"customerName"`
	Manufacturer    string   `json:"manufacturer"`
	Model           string   `json:"model"`
	Groups          []string `json:"groups"`
}

// Page adalah satu halaman hasil query.
type Page struct {
	Records []Device `json:"records"`
	Total   int      `json:"total"`
	Size    int      `json:"size"`
	Current int      `json:"current"`
	Pages   int      `json:"pages"`
}

// UnmarshalJSON menangani dua bentuk amplop data CMS yang ditemukan di
// lapangan & dokumentasi:
//
//	{"page": {"records": [...], "total": 3, ...}}   <- bentuk manual
//	{"records": [...], "total": 3, ...}             <- bentuk terlanjur diratakan
func (p *Page) UnmarshalJSON(data []byte) error {
	var wrapper struct {
		Page *Page `json:"page"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Page != nil {
		*p = *wrapper.Page
		return nil
	}

	type alias Page
	var flat alias
	if err := json.Unmarshal(data, &flat); err != nil {
		return err
	}
	*p = Page(flat)
	return nil
}

// PONInfo adalah data optik & status satu ONU dari CMS.
type PONInfo struct {
	PONMAC      string `json:"pon_mac"`
	GPONSN      string `json:"gpon_sn"`
	ONUState    int    `json:"onu_state"`
	Temp        string `json:"temp"`
	BiasCurrent string `json:"biasCurrent"`
	Voltage     string `json:"voltage"`
	RxPower     string `json:"rxPower"`
	TxPower     string `json:"txPower"`
	PONMode     string `json:"pon_mode"`
	LOID        string `json:"loid"`
	PONState    int    `json:"ponState"`

	PonErrorCause int `json:"PonErrorCause"`

	RxPowerLimit PowerLimit `json:"rxPowerLimit"`
	TxPowerLimit PowerLimit `json:"txPowerLimit"`
}

// PowerLimit adalah rentang nilai optik yang diperbolehkan.
type PowerLimit struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

// WAN adalah konfigurasi WAN satu ONU.
type WAN struct {
	Hidden          int    `json:"hiden"`
	EnableNAPT      int    `json:"enable_napt"`
	ConnectionType  int    `json:"connection_type"`
	VLANID          int    `json:"vlanId"`
	Deletable       int    `json:"deletable"`
	EnableVLAN      int    `json:"enable_vlan"`
	Interface       string `json:"interface"`
	IGMPProxy       int    `json:"enable_igmp_mld_proxy"`
	IPProtocol      int    `json:"ip_protocol"`
	MAC             string `json:"mac"`
	MTU             int    `json:"mtu"`
	ServiceType     int    `json:"service_type"`
	QoS8021Mark     int    `json:"802_1_mark"`
	MulticastVLANID int    `json:"multicast_vlan_id"`
	Username        string `json:"username"`
	ServiceName     string `json:"service_name"`
}

// WANConfig adalah konfigurasi WAN lengkap sebuah ONU.
type WANConfig struct {
	BindedPortList       []string `json:"binded_port_list"`
	BindedPortName       []string `json:"binded_port_name"`
	ValidPortBindingList []int    `json:"valid_port_binding_list"`
	WAN                  []WAN    `json:"wan"`
}

// ListOptions memfilter query daftar perangkat.
type ListOptions struct {
	Category     string // "OLT" atau "ONU" (default "ONU" di CMS)
	Manufacturer string
	Model        string
	RunningState string
	PageSize     int
	MaxRecords   int // batas total rekaman yang akan diambil (0 = semua)
}

// ListDevices mengambil satu halaman perangkat.
func (c *Client) ListDevices(ctx context.Context, opts ListOptions) (*Page, error) {
	if opts.Category == "" {
		opts.Category = "ONU"
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}

	q := url.Values{}
	q.Set("current", "1")
	q.Set("size", strconv.Itoa(opts.PageSize))
	q.Set("category", opts.Category)
	if opts.Manufacturer != "" {
		q.Set("manufacturer", opts.Manufacturer)
	}
	if opts.Model != "" {
		q.Set("model", opts.Model)
	}
	if opts.RunningState != "" {
		q.Set("runningState", opts.RunningState)
	}

	var page Page
	if err := c.get(ctx, "/v1/openapi/cms/devices/page", q, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// ListAllDevices mengambil seluruh rekaman dengan pagination.
func (c *Client) ListAllDevices(ctx context.Context, opts ListOptions) ([]Device, error) {
	if opts.PageSize <= 0 {
		opts.PageSize = 100
	}

	var out []Device
	current := 1
	for {
		q := url.Values{}
		q.Set("current", strconv.Itoa(current))
		q.Set("size", strconv.Itoa(opts.PageSize))
		q.Set("category", opts.Category)
		if opts.Manufacturer != "" {
			q.Set("manufacturer", opts.Manufacturer)
		}
		if opts.Model != "" {
			q.Set("model", opts.Model)
		}
		if opts.RunningState != "" {
			q.Set("runningState", opts.RunningState)
		}

		var page Page
		if err := c.get(ctx, "/v1/openapi/cms/devices/page", q, &page); err != nil {
			return out, err
		}
		out = append(out, page.Records...)

		if opts.MaxRecords > 0 && len(out) >= opts.MaxRecords {
			return out[:opts.MaxRecords], nil
		}
		if len(page.Records) < opts.PageSize || current >= page.Pages {
			return out, nil
		}
		current++
	}
}

// ListOLTs mengambil daftar OLT yang terdaftar di CMS.
func (c *Client) ListOLTs(ctx context.Context) ([]Device, error) {
	return c.ListAllDevices(ctx, ListOptions{Category: "OLT"})
}

// ListONUs mengambil daftar ONU yang terdaftar di CMS.
func (c *Client) ListONUs(ctx context.Context) ([]Device, error) {
	return c.ListAllDevices(ctx, ListOptions{Category: "ONU"})
}

// PONInfo mengambil data optik & status satu ONU.
func (c *Client) PONInfo(ctx context.Context, externalID string) (*PONInfo, error) {
	if strings.TrimSpace(externalID) == "" {
		return nil, fmt.Errorf("externalId kosong")
	}

	q := url.Values{}
	q.Set("externalId", externalID)

	var info PONInfo
	if err := c.get(ctx, "/v1/openapi/cms/devices/pon-info", q, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// WANConfig mengambil konfigurasi WAN satu ONU.
func (c *Client) WANConfig(ctx context.Context, externalID string) (*WANConfig, error) {
	if strings.TrimSpace(externalID) == "" {
		return nil, fmt.Errorf("externalId kosong")
	}

	q := url.Values{}
	q.Set("externalId", externalID)

	var cfg WANConfig
	if err := c.get(ctx, "/v1/openapi/cms/devices/wan-confignew", q, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// get melakukan GET dengan header X-Token dan membuka amplop respons.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	endpoint := c.BaseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("gagal membuat request: %w", err)
	}
	req.Header.Set("X-Token", c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("gagal memanggil CMS %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("gagal membaca respons CMS: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("respons CMS bukan JSON valid (HTTP %d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	if env.Code != 0 {
		return fmt.Errorf("CMS menolak permintaan (code %d): %s", env.Code, env.Msg)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("gagal mem-parse data CMS: %w", err)
	}
	return nil
}

// reOLTDisplay mem-parsing "FD1604S-B0(192.0.2.139)" menjadi model + IP.
//
// Field oltDisplay pada CMS memuat model perangkat dan alamat management-nya
// dalam satu string — sumber paling praktis untuk memetakan varian C-Data.
var reOLTDisplay = regexp.MustCompile(`^\s*([A-Za-z0-9\-()]+)\s*\(([0-9]{1,3}(?:\.[0-9]{1,3}){3})\)`)

// ParseOLTDisplay mem-parse field oltDisplay CMS.
func ParseOLTDisplay(display string) (model string, mgmtIP string) {
	m := reOLTDisplay.FindStringSubmatch(display)
	if m == nil {
		return strings.TrimSpace(display), ""
	}
	return m[1], m[2]
}

// Truncate memotong string untuk pesan error.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
