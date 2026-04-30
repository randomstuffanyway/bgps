package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"bgps/internal/geo"
	"bgps/internal/gpx"
	"bgps/internal/pack"
	"bgps/internal/tiles"
)

func main() {
	var (
		gpxPath     = flag.String("gpx", "", "input GPX route file")
		outFile     = flag.String("out", "offline_pack.tar.gz", "output pack archive (.tar.gz)")
		name        = flag.String("name", "Trekking", "pack display name")
		minZoom     = flag.Int("min-zoom", 14, "minimum tile zoom")
		maxZoom     = flag.Int("max-zoom", 18, "maximum tile zoom")
		defaultZoom = flag.Int("default-zoom", 17, "default viewer zoom")
		margin      = flag.Float64("margin", 0.15, "fractional map margin around route")
		tileURL     = flag.String("tile-url", "https://tile.openstreetmap.org/{z}/{x}/{y}.png", "XYZ tile URL template")
		workers     = flag.Int("workers", 6, "parallel tile downloads")
	)
	flag.Parse()
	if *gpxPath == "" {
		fmt.Fprintln(os.Stderr, "missing --gpx")
		os.Exit(1)
	}
	points, err := gpx.Load(*gpxPath)
	must(err)
	bounds := geo.Expand(geo.Bounds(points), *margin)
	centerLat, centerLon := geo.Center(bounds)
	tmpDir, err := os.MkdirTemp("", "bgps-pack-*")
	must(err)
	defer os.RemoveAll(tmpDir)
	tileDir := filepath.Join(tmpDir, "tiles")
	fmt.Printf("route points: %d\n", len(points))
	fmt.Printf("route length: %.2f km\n", geo.PathLengthMeters(points)/1000)
	fmt.Printf("downloading tiles z=%d..%d into %s\n", *minZoom, *maxZoom, tileDir)
	downloader := tiles.Downloader{URLTemplate: *tileURL, OutputDir: tileDir}
	must(downloader.DownloadBounds(bounds, *minZoom, *maxZoom, *workers))
	manifest := pack.Manifest{
		Name:        *name,
		SourceGPX:   filepath.Base(*gpxPath),
		RoutePoints: points,
		Bounds:      bounds,
		CenterLat:   centerLat,
		CenterLon:   centerLon,
		MinZoom:     *minZoom,
		MaxZoom:     *maxZoom,
		DefaultZoom: *defaultZoom,
		TileURL:     *tileURL,
		TileDir:     "tiles",
	}
	must(pack.Save(tmpDir, manifest))
	src, err := os.ReadFile(*gpxPath)
	must(err)
	must(os.WriteFile(filepath.Join(tmpDir, filepath.Base(*gpxPath)), src, 0o644))
	must(pack.CreateTarGz(tmpDir, *outFile))
	fmt.Printf("pack ready: %s\n", *outFile)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
