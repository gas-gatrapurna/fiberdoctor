package cms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListDevices memakai contoh respons VERBATIM dari manual
// "CMS Northbound Interface" (C-Data CMS 3.2).
func TestListDevices(t *testing.T) {
	body := `{
  "code": 0,
  "msg": "",
  "data": {
    "page": {
      "records": [
        {
          "deviceId": "00000000000000000000000000000001",
          "sn": "AAAAAAAAAAAAAAAA",
          "mac": "00:00:00:00:00:01",
          "channels": ["TR-069", "OMCI"],
          "deviceDesc": "demo-subscriber-07C",
          "deviceName": "ONT4GE2P1TVAS2WR_PON 0/0/3_ONU 4",
          "oltDisplay": "FD1604S-B0(192.0.2.139)",
          "ponId": "0/0/3",
          "onuId": "4",
          "parentId": "00000000000000000000000000000002",
          "customerName": "demo-subscriber",
          "deactive": false,
          "runningState": "online",
          "oltDeviceType": "physical",
          "oltRunningState": null,
          "externalId": "demo-subscriber"
        }
      ],
      "total": 1,
      "size": 10,
      "current": 1,
      "pages": 1
    }
  },
  "ext": null
}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") != "test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":701004,"msg":"API Key violation"}`))
			return
		}
		if r.URL.Path != "/v1/openapi/cms/devices/page" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("category") != "ONU" {
			t.Errorf("category = %q, want ONU", r.URL.Query().Get("category"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := New(srv.URL, "test-token")

	page, err := client.ListDevices(context.Background(), ListOptions{Category: "ONU"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Total != 1 || len(page.Records) != 1 {
		t.Fatalf("expected 1 record, got total=%d len=%d", page.Total, len(page.Records))
	}

	rec := page.Records[0]
	if rec.SN != "AAAAAAAAAAAAAAAA" {
		t.Errorf("SN = %q", rec.SN)
	}
	if rec.PONID != "0/0/3" || rec.ONUID != "4" {
		t.Errorf("ponId/onuId salah: %q / %q", rec.PONID, rec.ONUID)
	}
	if rec.CustomerName != "demo-subscriber" {
		t.Errorf("CustomerName = %q", rec.CustomerName)
	}
	if rec.RunningState != "online" {
		t.Errorf("RunningState = %q", rec.RunningState)
	}
	if rec.Deactive {
		t.Error("Deactive seharusnya false")
	}
	if len(rec.Channels) != 2 {
		t.Errorf("Channels = %v", rec.Channels)
	}

	model, ip := ParseOLTDisplay(rec.OLTDisplay)
	if model != "FD1604S-B0" {
		t.Errorf("model = %q, want FD1604S-B0", model)
	}
	if ip != "192.0.2.139" {
		t.Errorf("mgmtIP = %q, want 192.0.2.139", ip)
	}
}

func TestListDevicesAPIKeyViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":701004,"msg":"API Key violation","data":null}`))
	}))
	defer srv.Close()

	client := New(srv.URL, "wrong")
	if _, err := client.ListDevices(context.Background(), ListOptions{}); err == nil {
		t.Fatal("harus gagal saat API key ditolak")
	}
}

// TestPONInfo memakai contoh respons verbatim dari manual.
func TestPONInfo(t *testing.T) {
	body := `{
  "code": 0,
  "msg": "string",
  "data": {
    "pon_mac": "aabbccddeeff",
    "gpon_sn": "AAAAAAAAAAAAAAAA",
    "onu_state": 0,
    "temp": "41",
    "biasCurrent": "33",
    "voltage": "3.28",
    "rxPower": "-21.35",
    "rxPowerLimit": {"min": "-28", "max": "-8"},
    "txPower": "2.31",
    "txPowerLimit": {"min": "-5", "max": "5"},
    "pon_mode": "gpon",
    "loid": "demo-onu-loid",
    "PonErrorCause": 0,
    "ponState": 0
  }
}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/openapi/cms/devices/pon-info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("externalId") != "demo-onu-loid" {
			t.Errorf("externalId = %q", r.URL.Query().Get("externalId"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := New(srv.URL, "test-token")
	info, err := client.PONInfo(context.Background(), "demo-onu-loid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.GPONSN != "AAAAAAAAAAAAAAAA" {
		t.Errorf("GPONSN = %q", info.GPONSN)
	}
	if info.RxPower != "-21.35" {
		t.Errorf("RxPower = %q, want -21.35", info.RxPower)
	}
	if info.TxPower != "2.31" {
		t.Errorf("TxPower = %q", info.TxPower)
	}
	if info.RxPowerLimit.Min != "-28" || info.RxPowerLimit.Max != "-8" {
		t.Errorf("RxPowerLimit = %+v", info.RxPowerLimit)
	}
	if info.PONMode != "gpon" {
		t.Errorf("PONMode = %q", info.PONMode)
	}
}

func TestPONInfoEmptyExternalID(t *testing.T) {
	client := New("http://127.0.0.1:1", "x")
	if _, err := client.PONInfo(context.Background(), "  "); err == nil {
		t.Fatal("harus gagal untuk externalId kosong")
	}
}

func TestParseOLTDisplay(t *testing.T) {
	cases := []struct {
		in        string
		wantModel string
		wantIP    string
	}{
		{"FD1604S-B0(192.0.2.139)", "FD1604S-B0", "192.0.2.139"},
		{"FD1602S-B1(198.51.100.8)", "FD1602S-B1", "198.51.100.8"},
		{"OLT-CONCAT", "OLT-CONCAT", ""},
	}

	for _, tc := range cases {
		model, ip := ParseOLTDisplay(tc.in)
		if model != tc.wantModel {
			t.Errorf("ParseOLTDisplay(%q) model = %q, want %q", tc.in, model, tc.wantModel)
		}
		if ip != tc.wantIP {
			t.Errorf("ParseOLTDisplay(%q) ip = %q, want %q", tc.in, ip, tc.wantIP)
		}
	}
}
