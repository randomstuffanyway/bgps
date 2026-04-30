package gps

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bgps/internal/gpx"
	"go.bug.st/serial"
)

type Fix struct {
	Point     gpx.Point
	Time      time.Time
	Valid     bool
	SourceRaw string
}

type Reader struct {
	PortName string
	Baud     int

	mu          sync.RWMutex
	latest      Fix
	currentPort string
	status      string
}

func ListCandidatePorts() ([]string, error) {
	patterns := []string{"/dev/serial/by-id/*", "/dev/ttyUSB*", "/dev/ttyACM*"}
	seen := map[string]bool{}
	var out []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out, nil
}

func FirstAvailablePort(preferredBaud int) (string, int, error) {
	ports, err := ListCandidatePorts()
	if err != nil {
		return "", 0, err
	}
	if len(ports) == 0 {
		return "", 0, fmt.Errorf("no USB GPS serial port found")
	}
	for _, baud := range probeBauds(preferredBaud) {
		for _, port := range ports {
			ok, err := ProbePort(port, baud, 3*time.Second)
			if err == nil && ok {
				return port, baud, nil
			}
		}
	}
	return "", 0, fmt.Errorf("no GPS NMEA device found on candidate serial ports")
}

func (r *Reader) Latest() Fix {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.latest
}

func (r *Reader) CurrentPort() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.currentPort
}

func (r *Reader) Status() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *Reader) setLatest(f Fix) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latest = f
}

func (r *Reader) setConnectionState(port, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentPort = port
	r.status = status
}

func (r *Reader) OpenAndRead(onFix func(Fix)) error {
	if r.Baud == 0 {
		r.Baud = 4800
	}
	const retryDelay = 3 * time.Second
	for {
		portName, err := r.resolvePort()
		if err != nil {
			r.setConnectionState("", "waiting for GPS device")
			time.Sleep(retryDelay)
			continue
		}
		r.setConnectionState(portName, "connecting")
		if err := r.readFromPort(portName, onFix); err != nil {
			r.setConnectionState("", fmt.Sprintf("GPS disconnected, retrying: %v", err))
			time.Sleep(retryDelay)
			continue
		}
	}
}

func (r *Reader) resolvePort() (string, error) {
	if r.PortName == "" || r.PortName == "auto" {
		r.setConnectionState("", "probing GPS ports")
		port, baud, err := FirstAvailablePort(r.Baud)
		if err != nil {
			return "", err
		}
		r.Baud = baud
		return port, nil
	}
	return r.PortName, nil
}

func (r *Reader) readFromPort(portName string, onFix func(Fix)) error {
	mode := &serial.Mode{BaudRate: r.Baud}
	port, err := serial.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("open gps port %s: %w", portName, err)
	}
	defer port.Close()
	_ = port.SetReadTimeout(2 * time.Second)
	r.setConnectionState(portName, "connected")
	reader := bufio.NewReader(port)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if isTimeoutErr(err) {
				continue
			}
			return err
		}
		line = strings.TrimSpace(line)
		fix, ok := parseNMEA(line)
		if !ok {
			continue
		}
		r.setLatest(fix)
		if onFix != nil {
			onFix(fix)
		}
	}
}

func probeBauds(preferred int) []int {
	order := []int{preferred, 4800, 9600, 38400, 19200, 57600, 115200}
	seen := map[int]bool{}
	var out []int
	for _, baud := range order {
		if baud <= 0 || seen[baud] {
			continue
		}
		seen[baud] = true
		out = append(out, baud)
	}
	if len(out) == 0 {
		return []int{4800, 9600, 38400}
	}
	return out
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout")
}

func ProbePort(portName string, baud int, timeout time.Duration) (bool, error) {
	if baud == 0 {
		baud = 4800
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	port, err := serial.Open(portName, &serial.Mode{BaudRate: baud})
	if err != nil {
		return false, err
	}
	defer port.Close()
	_ = port.SetReadTimeout(500 * time.Millisecond)
	reader := bufio.NewReader(port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			if isTimeoutErr(err) {
				continue
			}
			return false, err
		}
		if isLikelyNMEA(strings.TrimSpace(line)) {
			return true, nil
		}
	}
	return false, nil
}

func isLikelyNMEA(line string) bool {
	if line == "" || line[0] != '$' {
		return false
	}
	star := strings.LastIndexByte(line, '*')
	if star < 0 || star+3 > len(line) {
		return false
	}
	if len(line) < 6 {
		return false
	}
	head := strings.SplitN(line[1:star], ",", 2)[0]
	if len(head) < 5 {
		return false
	}
	typ := head[len(head)-3:]
	switch typ {
	case "RMC", "GGA", "GSA", "GSV", "VTG", "GLL", "ZDA":
		return true
	default:
		return false
	}
}

func parseNMEA(line string) (Fix, bool) {
	var f Fix
	if line == "" || line[0] != '$' {
		return f, false
	}
	parts := strings.Split(line, ",")
	if len(parts) < 7 {
		return f, false
	}
	head := parts[0]
	switch {
	case strings.HasSuffix(head, "RMC"):
		if len(parts) < 7 || parts[2] != "A" {
			return f, false
		}
		lat, err1 := parseCoord(parts[3], parts[4])
		lon, err2 := parseCoord(parts[5], parts[6])
		if err1 != nil || err2 != nil {
			return f, false
		}
		f = Fix{Point: gpx.Point{Lat: lat, Lon: lon}, Time: time.Now(), Valid: true, SourceRaw: line}
		return f, true
	case strings.HasSuffix(head, "GGA"):
		if len(parts) < 7 || parts[6] == "0" {
			return f, false
		}
		lat, err1 := parseCoord(parts[2], parts[3])
		lon, err2 := parseCoord(parts[4], parts[5])
		if err1 != nil || err2 != nil {
			return f, false
		}
		f = Fix{Point: gpx.Point{Lat: lat, Lon: lon}, Time: time.Now(), Valid: true, SourceRaw: line}
		if len(parts) > 9 {
			if ele, err := strconv.ParseFloat(parts[9], 64); err == nil {
				f.Point.Ele = ele
			}
		}
		return f, true
	default:
		return f, false
	}
}

func parseCoord(raw, hemi string) (float64, error) {
	if raw == "" {
		return 0, fmt.Errorf("empty coordinate")
	}
	dot := strings.IndexByte(raw, '.')
	if dot < 0 || dot < 2 {
		return 0, fmt.Errorf("invalid coordinate %q", raw)
	}
	degDigits := dot - 2
	deg, err := strconv.ParseFloat(raw[:degDigits], 64)
	if err != nil {
		return 0, err
	}
	min, err := strconv.ParseFloat(raw[degDigits:], 64)
	if err != nil {
		return 0, err
	}
	v := deg + min/60.0
	if hemi == "S" || hemi == "W" {
		v = -v
	}
	return v, nil
}

func SaveRawSample(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
