# Contributing to fiberdoctor

Thanks for taking the time to contribute.

## Quick start

```bash
git clone https://github.com/gas-gatrapurna/fiberdoctor.git
cd fiberdoctor
go build ./...
go test ./...
```

The module has **zero external dependencies** - only the Go standard library.

## Before opening a pull request

```bash
gofmt -l .              # must print nothing
go vet ./...
go test ./... -race
```

## Adding a new OLT vendor

1. Create `pkg/driver/<vendor>/` with a driver implementing
   `driver.OLTDriver`.
2. Register the driver factory in `pkg/driver/registry.go`.
3. Add parser tests. If you add a CLI parser, add a fixture under
   `testdata/` and a golden test - never commit output captured from a real
   device, because it contains subscriber serial numbers and names.
4. Add the vendor to the table in `docs/ARCHITECTURE.md`.

## Test fixtures

Everything under `testdata/` is **synthetic**. Fixtures mimic the shape of
ZTE C320 CLI output but contain invented serial numbers, subscriber names and
locations. Keep it that way - real captures belong outside the repository.

## Commit messages

Use conventional prefixes: `feat:`, `fix:`, `docs:`, `test:`, `refactor:`.
