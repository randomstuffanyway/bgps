package geo

import (
	"math"

	"bgps/internal/gpx"
)

type BBox struct {
	MinLat float64 `json:"min_lat"`
	MinLon float64 `json:"min_lon"`
	MaxLat float64 `json:"max_lat"`
	MaxLon float64 `json:"max_lon"`
}

func Bounds(points []gpx.Point) BBox {
	b := BBox{
		MinLat: points[0].Lat,
		MinLon: points[0].Lon,
		MaxLat: points[0].Lat,
		MaxLon: points[0].Lon,
	}
	for _, p := range points[1:] {
		if p.Lat < b.MinLat {
			b.MinLat = p.Lat
		}
		if p.Lat > b.MaxLat {
			b.MaxLat = p.Lat
		}
		if p.Lon < b.MinLon {
			b.MinLon = p.Lon
		}
		if p.Lon > b.MaxLon {
			b.MaxLon = p.Lon
		}
	}
	return b
}

func Expand(b BBox, factor float64) BBox {
	latPad := (b.MaxLat - b.MinLat) * factor
	lonPad := (b.MaxLon - b.MinLon) * factor
	if latPad == 0 {
		latPad = 0.005
	}
	if lonPad == 0 {
		lonPad = 0.005
	}
	return BBox{
		MinLat: clampLat(b.MinLat - latPad),
		MaxLat: clampLat(b.MaxLat + latPad),
		MinLon: clampLon(b.MinLon - lonPad),
		MaxLon: clampLon(b.MaxLon + lonPad),
	}
}

func Center(b BBox) (lat, lon float64) {
	return (b.MinLat + b.MaxLat) / 2, (b.MinLon + b.MaxLon) / 2
}

func clampLat(v float64) float64 {
	if v < -85.05112878 {
		return -85.05112878
	}
	if v > 85.05112878 {
		return 85.05112878
	}
	return v
}

func clampLon(v float64) float64 {
	if v < -180 {
		return -180
	}
	if v > 180 {
		return 180
	}
	return v
}

func LatLonToWorld(lat, lon float64, zoom int) (float64, float64) {
	size := float64(uint(1) << zoom * 256)
	x := (lon + 180.0) / 360.0 * size
	sinLat := math.Sin(lat * math.Pi / 180.0)
	y := (0.5 - math.Log((1+sinLat)/(1-sinLat))/(4*math.Pi)) * size
	return x, y
}

func WorldToLatLon(x, y float64, zoom int) (float64, float64) {
	size := float64(uint(1) << zoom * 256)
	lon := x/size*360.0 - 180.0
	n := math.Pi - 2.0*math.Pi*y/size
	lat := 180.0 / math.Pi * math.Atan(0.5*(math.Exp(n)-math.Exp(-n)))
	return lat, lon
}

func TileXY(lat, lon float64, zoom int) (int, int) {
	x, y := LatLonToWorld(lat, lon, zoom)
	return int(x) / 256, int(y) / 256
}

func DistanceMeters(a, b gpx.Point) float64 {
	const earth = 6371000.0
	lat1 := a.Lat * math.Pi / 180.0
	lat2 := b.Lat * math.Pi / 180.0
	dLat := (b.Lat - a.Lat) * math.Pi / 180.0
	dLon := (b.Lon - a.Lon) * math.Pi / 180.0
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earth * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
}

func PathLengthMeters(points []gpx.Point) float64 {
	var total float64
	for i := 1; i < len(points); i++ {
		total += DistanceMeters(points[i-1], points[i])
	}
	return total
}
