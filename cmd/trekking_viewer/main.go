package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bgps/internal/geo"
	"bgps/internal/gps"
	"bgps/internal/gpx"
	"bgps/internal/pack"
	"bgps/internal/tiles"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

type button struct {
	Label string
	X     int
	Y     int
	W     int
	H     int
	OnTap func()
}

type trackRecorder struct {
	path       string
	mu         sync.Mutex
	points     []gpx.Point
	dirty      bool
	lastSave   time.Time
	minSpacing float64
}

func (t *trackRecorder) Add(p gpx.Point) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.points) > 0 && geo.DistanceMeters(t.points[len(t.points)-1], p) < t.minSpacing {
		return
	}
	t.points = append(t.points, p)
	t.dirty = true
}

func (t *trackRecorder) Snapshot() []gpx.Point {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]gpx.Point, len(t.points))
	copy(out, t.points)
	return out
}

func (t *trackRecorder) SaveIfNeeded(force bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !force {
		if !t.dirty || time.Since(t.lastSave) < 5*time.Second {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return err
	}
	if err := gpx.WriteTrack(t.path, t.points); err != nil {
		return err
	}
	t.lastSave = time.Now()
	t.dirty = false
	return nil
}

type tileCache struct {
	root string
	mu   sync.Mutex
	img  map[string]*ebiten.Image
}

func newTileCache(root string) *tileCache {
	return &tileCache{root: root, img: map[string]*ebiten.Image{}}
}

func (c *tileCache) Get(z, x, y int) *ebiten.Image {
	key := fmt.Sprintf("%d/%d/%d", z, x, y)
	c.mu.Lock()
	if img, ok := c.img[key]; ok {
		c.mu.Unlock()
		return img
	}
	c.mu.Unlock()
	src, err := tiles.LoadImage(c.root, z, x, y)
	if err != nil {
		return nil
	}
	img := ebiten.NewImageFromImage(src)
	c.mu.Lock()
	c.img[key] = img
	c.mu.Unlock()
	return img
}

type Game struct {
	manifest   pack.Manifest
	cache      *tileCache
	gpsReader  *gps.Reader
	track      *trackRecorder
	zoom       int
	centerLat  float64
	centerLon  float64
	followGPS  bool
	fullscreen bool
	status     string
	lastFix    gps.Fix
	lastFixMu  sync.RWMutex
	buttons    []button
	mouseDown  bool
	lastMouseX int
	lastMouseY int
	clickArmed bool
	width      int
	height     int
	keyLatch   map[ebiten.Key]bool
}

func newGame(manifest pack.Manifest, packDir string, gpsReader *gps.Reader, track *trackRecorder, fullscreen bool, width, height int) *Game {
	g := &Game{
		manifest:   manifest,
		cache:      newTileCache(filepath.Join(packDir, manifest.TileDir)),
		gpsReader:  gpsReader,
		track:      track,
		zoom:       manifest.DefaultZoom,
		centerLat:  manifest.CenterLat,
		centerLon:  manifest.CenterLon,
		followGPS:  true,
		fullscreen: fullscreen,
		width:      width,
		height:     height,
		keyLatch:   map[ebiten.Key]bool{},
	}
	g.rebuildButtons()
	return g
}

func (g *Game) rebuildButtons() {
	pad := 8
	bw := 100
	bh := 34
	footerH := 28
	bottomGap := 12
	baseY := g.height - footerH - bottomGap
	g.buttons = []button{
		{Label: "+", X: pad, Y: pad, W: bh, H: bh, OnTap: func() { g.changeZoom(1) }},
		{Label: "-", X: pad, Y: pad + bh + pad, W: bh, H: bh, OnTap: func() { g.changeZoom(-1) }},
		{Label: "FOLLOW", X: pad, Y: baseY - (bh+pad)*3, W: bw, H: bh, OnTap: func() { g.followGPS = !g.followGPS }},
		{Label: "CENTER", X: pad, Y: baseY - (bh+pad)*2, W: bw, H: bh, OnTap: func() { g.centerOnGPS() }},
		{Label: "FULL", X: pad, Y: baseY - (bh + pad), W: bw, H: bh, OnTap: func() { g.toggleFullscreen() }},
	}
}

func (g *Game) setFix(f gps.Fix) {
	g.lastFixMu.Lock()
	g.lastFix = f
	g.lastFixMu.Unlock()
	if f.Valid {
		g.track.Add(f.Point)
		if g.followGPS {
			g.centerLat = f.Point.Lat
			g.centerLon = f.Point.Lon
		}
	}
}

func (g *Game) getFix() gps.Fix {
	g.lastFixMu.RLock()
	defer g.lastFixMu.RUnlock()
	return g.lastFix
}

func (g *Game) Update() error {
	g.handleKeys()
	g.handleMouse()
	if err := g.track.SaveIfNeeded(false); err != nil {
		g.status = err.Error()
	}
	fix := g.getFix()
	status := fmt.Sprintf("%s | zoom %d | follow %v", g.manifest.Name, g.zoom, g.followGPS)
	if fix.Valid {
		d := nearestDistanceMeters(fix.Point, g.manifest.RoutePoints)
		status += fmt.Sprintf(" | GPS %.5f, %.5f | route dist %.0fm | track pts %d", fix.Point.Lat, fix.Point.Lon, d, len(g.track.Snapshot()))
	} else if g.gpsReader != nil {
		gpsStatus := g.gpsReader.Status()
		gpsPort := g.gpsReader.CurrentPort()
		if gpsStatus == "" {
			gpsStatus = "waiting for GPS"
		}
		if gpsPort != "" {
			status += fmt.Sprintf(" | GPS %s on %s", gpsStatus, gpsPort)
		} else {
			status += fmt.Sprintf(" | GPS %s", gpsStatus)
		}
	}
	g.status = status
	return nil
}

func (g *Game) handleKeys() {
	g.onPress(ebiten.KeyEqual, func() { g.changeZoom(1) })
	g.onPress(ebiten.KeyKPAdd, func() { g.changeZoom(1) })
	g.onPress(ebiten.KeyMinus, func() { g.changeZoom(-1) })
	g.onPress(ebiten.KeyKPSubtract, func() { g.changeZoom(-1) })
	g.onPress(ebiten.KeyF11, g.toggleFullscreen)
	g.onPress(ebiten.KeyC, g.centerOnGPS)
	g.onPress(ebiten.KeyF, func() { g.followGPS = !g.followGPS })

	_, wheelY := ebiten.Wheel()
	if wheelY > 0 {
		g.changeZoom(1)
	}
	if wheelY < 0 {
		g.changeZoom(-1)
	}

	pan := 20.0
	if ebiten.IsKeyPressed(ebiten.KeyArrowLeft) || ebiten.IsKeyPressed(ebiten.KeyA) {
		g.panPixels(-pan, 0)
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowRight) || ebiten.IsKeyPressed(ebiten.KeyD) {
		g.panPixels(pan, 0)
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowUp) || ebiten.IsKeyPressed(ebiten.KeyW) {
		g.panPixels(0, -pan)
	}
	if ebiten.IsKeyPressed(ebiten.KeyArrowDown) || ebiten.IsKeyPressed(ebiten.KeyS) {
		g.panPixels(0, pan)
	}
}

func (g *Game) onPress(key ebiten.Key, fn func()) {
	pressed := ebiten.IsKeyPressed(key)
	if pressed && !g.keyLatch[key] {
		fn()
	}
	g.keyLatch[key] = pressed
}

func (g *Game) handleMouse() {
	x, y := ebiten.CursorPosition()
	pressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	if pressed && !g.mouseDown {
		g.mouseDown = true
		g.lastMouseX, g.lastMouseY = x, y
		g.clickArmed = true
	}
	if pressed && g.mouseDown {
		dx := x - g.lastMouseX
		dy := y - g.lastMouseY
		if dx != 0 || dy != 0 {
			g.panPixels(float64(-dx), float64(-dy))
			g.lastMouseX, g.lastMouseY = x, y
			if abs(dx)+abs(dy) > 4 {
				g.clickArmed = false
			}
		}
	}
	if !pressed && g.mouseDown {
		g.mouseDown = false
		if g.clickArmed {
			for _, b := range g.buttons {
				if x >= b.X && x <= b.X+b.W && y >= b.Y && y <= b.Y+b.H {
					b.OnTap()
					break
				}
			}
		}
	}
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{240, 240, 240, 255})
	g.drawTiles(screen)
	g.drawRoute(screen)
	g.drawTrack(screen)
	g.drawGPS(screen)
	g.drawHUD(screen)
}

func (g *Game) drawTiles(screen *ebiten.Image) {
	cx, cy := geo.LatLonToWorld(g.centerLat, g.centerLon, g.zoom)
	left := cx - float64(g.width)/2
	top := cy - float64(g.height)/2
	startX := int(math.Floor(left / tiles.TileSize))
	startY := int(math.Floor(top / tiles.TileSize))
	endX := int(math.Ceil((left + float64(g.width)) / tiles.TileSize))
	endY := int(math.Ceil((top + float64(g.height)) / tiles.TileSize))
	for tx := startX; tx <= endX; tx++ {
		for ty := startY; ty <= endY; ty++ {
			img := g.cache.Get(g.zoom, tx, ty)
			dx := float64(tx*tiles.TileSize) - left
			dy := float64(ty*tiles.TileSize) - top
			if img == nil {
				ebitenutil.DrawRect(screen, dx, dy, tiles.TileSize, tiles.TileSize, color.RGBA{220, 220, 220, 255})
				continue
			}
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(dx, dy)
			screen.DrawImage(img, op)
		}
	}
}

func (g *Game) drawRoute(screen *ebiten.Image) {
	if len(g.manifest.RoutePoints) < 2 {
		return
	}
	for i := 1; i < len(g.manifest.RoutePoints); i++ {
		x1, y1 := g.screenPoint(g.manifest.RoutePoints[i-1])
		x2, y2 := g.screenPoint(g.manifest.RoutePoints[i])
		ebitenutil.DrawLine(screen, x1, y1, x2, y2, color.RGBA{220, 0, 0, 255})
		ebitenutil.DrawLine(screen, x1+1, y1, x2+1, y2, color.RGBA{220, 0, 0, 255})
	}
}

func (g *Game) drawTrack(screen *ebiten.Image) {
	pts := g.track.Snapshot()
	if len(pts) < 2 {
		return
	}
	for i := 1; i < len(pts); i++ {
		x1, y1 := g.screenPoint(pts[i-1])
		x2, y2 := g.screenPoint(pts[i])
		ebitenutil.DrawLine(screen, x1, y1, x2, y2, color.RGBA{0, 80, 220, 255})
	}
}

func (g *Game) drawGPS(screen *ebiten.Image) {
	fix := g.getFix()
	if !fix.Valid {
		return
	}
	x, y := g.screenPoint(fix.Point)
	ebitenutil.DrawRect(screen, x-5, y-5, 10, 10, color.RGBA{0, 160, 0, 255})
	ebitenutil.DrawRect(screen, x-1, y-12, 2, 24, color.White)
	ebitenutil.DrawRect(screen, x-12, y-1, 24, 2, color.White)
}

func (g *Game) drawHUD(screen *ebiten.Image) {
	ebitenutil.DrawRect(screen, 0, float64(g.height-28), float64(g.width), 28, color.RGBA{0, 0, 0, 180})
	ebitenutil.DebugPrintAt(screen, g.status, 8, g.height-22)
	for _, b := range g.buttons {
		fill := color.RGBA{30, 30, 30, 180}
		if b.Label == "FOLLOW" && g.followGPS {
			fill = color.RGBA{0, 100, 0, 200}
		}
		ebitenutil.DrawRect(screen, float64(b.X), float64(b.Y), float64(b.W), float64(b.H), fill)
		ebitenutil.DebugPrintAt(screen, b.Label, b.X+10, b.Y+10)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	if outsideWidth > 0 {
		g.width = outsideWidth
	}
	if outsideHeight > 0 {
		g.height = outsideHeight
	}
	g.rebuildButtons()
	return g.width, g.height
}

func (g *Game) screenPoint(p gpx.Point) (float64, float64) {
	cx, cy := geo.LatLonToWorld(g.centerLat, g.centerLon, g.zoom)
	px, py := geo.LatLonToWorld(p.Lat, p.Lon, g.zoom)
	return px - cx + float64(g.width)/2, py - cy + float64(g.height)/2
}

func (g *Game) panPixels(dx, dy float64) {
	g.followGPS = false
	cx, cy := geo.LatLonToWorld(g.centerLat, g.centerLon, g.zoom)
	lat, lon := geo.WorldToLatLon(cx+dx, cy+dy, g.zoom)
	g.centerLat, g.centerLon = lat, lon
}

func (g *Game) changeZoom(delta int) {
	nz := g.zoom + delta
	if nz < g.manifest.MinZoom {
		nz = g.manifest.MinZoom
	}
	if nz > g.manifest.MaxZoom {
		nz = g.manifest.MaxZoom
	}
	if nz == g.zoom {
		return
	}
	g.zoom = nz
}

func (g *Game) centerOnGPS() {
	fix := g.getFix()
	if fix.Valid {
		g.centerLat = fix.Point.Lat
		g.centerLon = fix.Point.Lon
		g.followGPS = true
		return
	}
	g.centerLat = g.manifest.CenterLat
	g.centerLon = g.manifest.CenterLon
	g.followGPS = false
}

func (g *Game) toggleFullscreen() {
	g.fullscreen = !g.fullscreen
	ebiten.SetFullscreen(g.fullscreen)
}

func nearestDistanceMeters(p gpx.Point, route []gpx.Point) float64 {
	if len(route) == 0 {
		return 0
	}
	best := math.MaxFloat64
	for _, rp := range route {
		d := geo.DistanceMeters(p, rp)
		if d < best {
			best = d
		}
	}
	return best
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func main() {
	var (
		packPath   = flag.String("pack", "offline_pack.tar.gz", "offline pack archive (.tar.gz) or directory")
		port       = flag.String("port", "auto", "GPS serial port or auto")
		baud       = flag.Int("baud", 4800, "GPS serial baud")
		trackOut   = flag.String("track-out", "", "recorded GPX file path")
		width      = flag.Int("width", 1024, "window width")
		height     = flag.Int("height", 700, "window height")
		fullscreen = flag.Bool("fullscreen", false, "start full screen")
	)
	flag.Parse()

	packDir, cleanup, err := openPack(*packPath)
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	manifest, err := pack.Load(packDir)
	if err != nil {
		log.Fatal(err)
	}
	if *trackOut == "" {
		stamp := time.Now().Format("20060102-150405")
		base := strings.TrimSuffix(filepath.Base(*packPath), ".tar.gz")
		if base == filepath.Base(*packPath) {
			base = strings.TrimSuffix(base, filepath.Ext(base))
		}
		trackRoot := filepath.Join(filepath.Dir(*packPath), base+"-tracks")
		*trackOut = filepath.Join(trackRoot, "track-"+stamp+".gpx")
	}
	rec := &trackRecorder{path: *trackOut, minSpacing: 3}
	reader := &gps.Reader{PortName: *port, Baud: *baud}
	game := newGame(manifest, packDir, reader, rec, *fullscreen, *width, *height)
	go func() {
		if err := reader.OpenAndRead(game.setFix); err != nil {
			game.status = err.Error()
		}
	}()
	ebiten.SetWindowTitle("Trekking Viewer")
	ebiten.SetWindowSize(*width, *height)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetFullscreen(*fullscreen)
	defer func() { _ = rec.SaveIfNeeded(true) }()
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}

func openPack(packPath string) (string, func(), error) {
	info, err := os.Stat(packPath)
	if err != nil {
		return "", func() {}, err
	}
	if info.IsDir() {
		dir, err := findManifestDir(packPath)
		return dir, func() {}, err
	}
	if !strings.HasSuffix(strings.ToLower(packPath), ".tar.gz") {
		return "", func() {}, fmt.Errorf("pack must be directory or .tar.gz archive")
	}
	tmpDir, err := os.MkdirTemp("", "bgps-view-*")
	if err != nil {
		return "", func() {}, err
	}
	if err := pack.ExtractTarGz(packPath, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", func() {}, err
	}
	dir, err := findManifestDir(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", func() {}, err
	}
	return dir, func() { _ = os.RemoveAll(tmpDir) }, nil
}

func findManifestDir(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err == nil {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(candidate, "manifest.json")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("unable to find manifest.json in pack")
}
