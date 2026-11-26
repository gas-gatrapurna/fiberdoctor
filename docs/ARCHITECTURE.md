# Architecture

`fiberdoctor` is split into three layers so that vendor-specific knowledge stays
in one place and never leaks into the rules that decide what to do.

```
cmd/fd_doctor        cmd/fd_golden        (CLI entry points)
        |                    |
        |                    +--> capture planner (read-only "show " guard)
        |
        v
pkg/driver           driver contract + registry
  |-- zte/            ZTE C320 driver + CLI parsers
  |-- cdata/          C-Data FD-series driver
  |    `-- cms/       C-Data CMS northbound API client
  `-- cli/            generic CLI-over-Telnet session
        |
        v
pkg/triage           optical classification, blast radius
pkg/gis              haversine distance, nearest ODP
```

## The driver contract

Every vendor package implements one interface (`pkg/driver/driver.go`) and
registers itself from an `init()` function. Nothing imports a vendor package
except the registry and the CLI entry points, so adding a vendor never requires
changing core code.

```go
type OLTDriver interface {
    Connect(ctx context.Context) error
    Disconnect(ctx context.Context) error
    ExecuteRawCLI(ctx context.Context, cmd string) (string, error)
    ListONUs(ctx context.Context) ([]ONURecord, error)
    GetSummary(ctx context.Context) (*OLTSummary, error)
}
```

## Parsers are pure functions

CLI output is turned into structs by free functions that take a string and
return data. They never open a socket, never read configuration and never log.
That is what makes them testable against a fixture file, and it is why the
parsers have no dependency on the driver.

| Function | Input | Output |
|----------|-------|--------|
| `ParseZTEONUState` | `show gpon onu state` | ONU records with phase state |
| `ParseZTERegistry` | `show running-config` | map `slot/subslot/port:index` -> SN |
| `ParseZTERunningConfig` | `show running-config` | ONU records with T-CONT profile and service VLANs |
| `ParseZTESystemGroup` | `show system-group` | model, firmware, name, location, uptime |
| `ParseZTECard` | `show card` | card inventory |
| `ParseZTECardTemperature` | `show card-temperature` | slot -> degrees Celsius |
| `ParseZTEOpticalPowerBulk` | `show pon power olt-rx \| onu-rx` | ONU id -> optical reading |
| `ParseZTEOpticalPower` | `show pon power attenuation <iface>` | single ONU optical budget |

## Why the registry is a separate parser

The `show gpon onu state` table on a real C320 does not carry serial numbers.
The SN-to-ONU mapping only exists in `show running-config`, inside the
`interface gpon-olt_<slot>/<subslot>/<port>` block. Parsing the two
independently - and then asserting that every ONU in the state table resolves
to a registry entry - is what keeps the inventory honest.

## Optical power, in bulk

Reading optical power one ONU at a time means one command per subscriber.
Reading it per PON port means two commands per port (`olt-rx` and `onu-rx`),
which is the difference between a few dozen commands and several hundred on a
busy port. `OpticalBulkReader` is an optional interface a driver may implement.

## Safety model

* `fd_golden` refuses to send anything that does not start with `show `. The
  check runs before the command reaches the device.
* Credentials are never written to disk by the tools. The password comes from
  `FD_OLT_PASS` or from a file referenced by `FD_CRED_VAULT`.
* Captures contain subscriber identifiers. Keep them out of version control -
  `data/` is gitignored for that reason.

## Test strategy

Parsers are verified against committed synthetic fixtures under `testdata/`.
Because the fixtures are deterministic, the tests assert exact counts rather
than ranges. A missing fixture is a test failure, not a skip - a skipped
parser test is indistinguishable from a broken one.
