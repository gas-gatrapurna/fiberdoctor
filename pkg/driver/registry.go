package driver

import (
	"fmt"
	"sync"
)

type DriverFactory func(cfg OLTConfig) (OLTDriver, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]DriverFactory)
)

// RegisterDriver mendaftarkan factory driver untuk tipe vendor tertentu.
func RegisterDriver(vendorKey string, factory DriverFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[vendorKey] = factory
}

// GetDriver menginisialisasi driver yang sesuai berdasarkan vendor pada OLTConfig.
func GetDriver(cfg OLTConfig) (OLTDriver, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	factory, ok := registry[cfg.Vendor]
	if !ok {
		return nil, fmt.Errorf("driver untuk vendor '%s' belum terdaftar", cfg.Vendor)
	}
	return factory(cfg)
}
