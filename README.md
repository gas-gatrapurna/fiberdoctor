# fiberdoctor

> **FTTH OLT diagnostics toolkit** - vendor-neutral parsers, optical triage and GIS
> helpers for GPON access networks. Go, standard library only, zero dependencies.

[![CI](https://github.com/gas-gatrapurna/fiberdoctor/actions/workflows/ci.yml/badge.svg)](https://github.com/gas-gatrapurna/fiberdoctor/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.23%2B-blue.svg)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## What it is

Access networks are hard to automate. Every OLT vendor ships a different CLI,
a different output format and a different idea of what an ONU record looks like.
Operators end up writing one-off scripts per site, and the knowledge of how a
particular chassis behaves usually lives in the head of one field engineer.

`fiberdoctor` turns that into reusable Go packages:

| Package | Purpose |
|---------|---------|
| `pkg/driver/zte` | ZTE C320 driver + parsers for `show gpon onu state`, `show running-config`, `show card`, `show card-temperature`, `show system-group`, bulk optical power |
| `pkg/driver/cdata` | C-Data FD-series driver (GPON/EPON) plus a C-Data CMS northbound API client |
| `pkg/driver/cli` | Generic linear CLI-over-Telnet session with prompt detection and paging handling |
| `pkg/triage` | Optical budget classification, drop-vs-ODC reasoning, blast-radius analysis |
| `pkg/gis` | Haversine distance and nearest-ODP lookup |

Two command line tools sit on top:

* `cmd/fd_doctor` - live NOC health check for a single OLT: chassis summary,
  card inventory, temperature, ONU inventory and per-PON-port fault triage.
* `cmd/fd_golden` - read-only CLI capture used to build parser fixtures.

---

## Why the parsers are the point

The hard part of FTTH automation is not connecting to the device - it is
parsing what comes back, and parsing it *correctly*. A few things this code
gets right that naive scrapers do not:

* The `show gpon onu state` table on a real ZTE C320 does **not** contain
  serial numbers. The SN-to-ONU mapping only exists in
  `show running-config`, under the `interface gpon-olt_<slot>/<subslot>/<port>`
  block. The registry parser and the state parser are therefore separate, and
  a golden test asserts that every ONU in the state table resolves to an
  entry in the registry.
* ZTE firmware appends NUL bytes to parts of the output. Every line is
  sanitised before parsing.
* Optical power is read in **bulk** - two commands per PON port instead of
  one per ONU. On a fully loaded port that is the difference between a few
  dozen commands and several hundred.
* Optical readings distinguish "no signal" from a numeric value. A missing
  reading is not the same as `-30 dBm`, and the triage logic treats them
  differently.

---

## Build and test

```bash
git clone https://github.com/gas-gatrapurna/fiberdoctor.git
cd fiberdoctor
go build ./...
go test ./...
```

There are no third-party dependencies, so `go build` works offline and the
build is deterministic.

Before opening a pull request:

```bash
gofmt -l .            # must print nothing
go vet ./...
go test ./... -race
```

---

## Usage

### Live health check

```bash
export FD_OLT_PASS='your-password'      # or set FD_CRED_VAULT to a credentials file
go run ./cmd/fd_doctor -host 192.0.2.10 -user olt-admin -name my-olt
```

`--host`, `--user` and `--name` are mandatory. The password is read from
`FD_OLT_PASS`; if that is unset, the credential file given by `--vault` or
by the `FD_CRED_VAULT` environment variable is parsed instead. No credential
is ever stored by the tool.

### Capturing a fixture

```bash
export FD_OLT_PASS='your-password'
go run ./cmd/fd_golden -host 192.0.2.10 -user olt-admin -name my-olt \
    --outdir /tmp/my-olt-capture
```

`fd_golden` enforces a read-only guard: **only** commands starting with
`show ` are sent. Anything else is rejected before the device is touched.

> Captures contain subscriber serial numbers, names and locations. Keep them
> outside version control - see `SECURITY.md`.

---

## Test fixtures

Everything under `testdata/` is **synthetic**. The fixtures mimic the shape of
ZTE C320 CLI output - column headers, `no signal` tokens, and the exact column
alignment the parsers rely on - but every serial number, subscriber name, VLAN
and location in them is invented.

```
testdata/
├── inventory_synth.json        # 5 fictional ONU records
├── odp_synth.json              # 3 fictional ODP points
└── zte_c320_synth/
    ├── olt-synth-a/            # CLI state, running-config, cards, optics
    └── olt-synth-b/
```

Because the fixtures are committed, the golden tests run on every machine and
in CI - no device access and no skipped tests. A fixture that goes missing is
a test **failure**, not a skip.

---

## Repository layout

```
fiberdoctor/
├── cmd/
│   ├── fd_doctor/              # live NOC health check
│   └── fd_golden/              # read-only CLI capture tool
├── pkg/
│   ├── driver/                 # driver contract + registry
│   │   ├── zte/                # ZTE C320 driver and parsers
│   │   ├── cdata/              # C-Data driver (+ cms northbound client)
│   │   └── cli/                # generic CLI-over-Telnet session
│   ├── triage/                 # optical classification, blast radius
│   └── gis/                    # haversine, nearest ODP
├── testdata/                   # synthetic fixtures (no real data)
├── docs/ARCHITECTURE.md
└── go.mod                      # module github.com/gas-gatrapurna/fiberdoctor
```

---

## Roadmap

* [x] ZTE C320 driver and parsers
* [x] C-Data FD-series driver and CMS northbound client
* [x] Generic CLI-over-Telnet session
* [x] Optical triage and blast radius
* [x] GIS helpers
* [ ] Huawei MA-series driver
* [ ] SNMP poller daemon
* [ ] GIS importer for ODP locations
* [ ] REST API server

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). New vendor drivers are welcome -
please read the "Adding a new OLT vendor" section first.

## Security

See [SECURITY.md](SECURITY.md). Please report vulnerabilities privately.

## License

[MIT](LICENSE) - use it, fork it, ship it. Attribution appreciated.
