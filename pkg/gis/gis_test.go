package gis

import (
	"math"
	"testing"
)

func TestHaversineDistanceM(t *testing.T) {
	// Jarak antara 2 titik berdekatan.
	lat1, lon1 := -1.0000, 2.0000
	lat2, lon2 := -1.0002, 2.0000

	dist := HaversineDistanceM(lat1, lon1, lat2, lon2)
	if dist <= 0 || dist > 50 {
		t.Errorf("jarak tidak masuk akal: %f meter", dist)
	}
}

func TestFindNearestODPs(t *testing.T) {
	userLat, userLng := -1.0000, 2.0000

	odps := []ODPRecord{
		{ID: "ODP-01", Name: "ODP-Dekat", Latitude: -1.0001, Longitude: 2.0001, Status: "idle", UsedPorts: 2, TotalPorts: 8},
		{ID: "ODP-02", Name: "ODP-Jauh", Latitude: -1.0072, Longitude: 2.0030, Status: "idle", UsedPorts: 4, TotalPorts: 8},
	}

	results := FindNearestODPs(userLat, userLng, odps, 200.0, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 ODP within 200m, got %d", len(results))
	}

	if results[0].ODP.ID != "ODP-01" {
		t.Errorf("expected nearest ODP-01, got %s", results[0].ODP.ID)
	}

	if math.Abs(results[0].AirDistanceM-15.0) > 10.0 {
		t.Errorf("unexpected distance: %f", results[0].AirDistanceM)
	}
}
