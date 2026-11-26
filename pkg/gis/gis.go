package gis

import (
	"math"
	"sort"
)

// ODPRecord merepresentasikan titik Optical Distribution Point (ODP).
type ODPRecord struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Latitude   float64 `json:"lat"`
	Longitude  float64 `json:"lng"`
	Status     string  `json:"status"` // idle, partial, full
	UsedPorts  int     `json:"used_ports"`
	TotalPorts int     `json:"total_ports"`
	FeederID   string  `json:"feeder_id,omitempty"`
	Address    string  `json:"address,omitempty"`
}

// DistanceResult hasil perhitungan jarak ke ODP.
type DistanceResult struct {
	ODP          ODPRecord `json:"odp"`
	AirDistanceM float64   `json:"air_distance_m"`
}

// HaversineDistanceM menghitung jarak udara garis lurus antara dua koordinat geografis (meter).
func HaversineDistanceM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusM = 6371000.0 // Radius bumi dalam meter

	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(rLat1)*math.Cos(rLat2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusM * c
}

// FindNearestODPs mencari daftar ODP terdekat dari suatu titik koordinat dalam radius tertentu.
func FindNearestODPs(userLat, userLng float64, odps []ODPRecord, maxDistanceM float64, topN int) []DistanceResult {
	var candidates []DistanceResult

	for _, odp := range odps {
		dist := HaversineDistanceM(userLat, userLng, odp.Latitude, odp.Longitude)
		if dist <= maxDistanceM {
			candidates = append(candidates, DistanceResult{
				ODP:          odp,
				AirDistanceM: math.Round(dist*10) / 10,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].AirDistanceM < candidates[j].AirDistanceM
	})

	if len(candidates) > topN {
		return candidates[:topN]
	}
	return candidates
}
