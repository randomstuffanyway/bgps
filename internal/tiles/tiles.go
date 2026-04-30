package tiles

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bgps/internal/geo"
)

const TileSize = 256

type Downloader struct {
	URLTemplate string
	OutputDir   string
	Client      *http.Client
}

type Job struct {
	Z int
	X int
	Y int
}

func (d Downloader) DownloadBounds(b geo.BBox, minZoom, maxZoom, workers int) error {
	if d.Client == nil {
		d.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan Job, workers*2)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if err := d.Download(job.Z, job.X, job.Y); err != nil {
					select {
					case errCh <- err:
					default:
					}
				}
			}
		}()
	}

	for z := minZoom; z <= maxZoom; z++ {
		minX, minY := geo.TileXY(b.MaxLat, b.MinLon, z)
		maxX, maxY := geo.TileXY(b.MinLat, b.MaxLon, z)
		for x := minX; x <= maxX; x++ {
			for y := minY; y <= maxY; y++ {
				jobs <- Job{Z: z, X: x, Y: y}
			}
		}
	}
	close(jobs)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (d Downloader) Download(z, x, y int) error {
	path := filepath.Join(d.OutputDir, fmt.Sprintf("%d/%d/%d.png", z, x, y))
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	url := strings.NewReplacer(
		"{z}", fmt.Sprintf("%d", z),
		"{x}", fmt.Sprintf("%d", x),
		"{y}", fmt.Sprintf("%d", y),
	).Replace(d.URLTemplate)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "bgps/0.1 (+offline trekking map)")
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tile %d/%d/%d: status %s", z, x, y, resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func TilePath(root string, z, x, y int) string {
	return filepath.Join(root, fmt.Sprintf("%d/%d/%d.png", z, x, y))
}

func LoadImage(root string, z, x, y int) (image.Image, error) {
	f, err := os.Open(TilePath(root, z, x, y))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}
