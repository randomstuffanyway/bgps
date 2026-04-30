package gpx

import (
	"encoding/xml"
	"fmt"
	"os"
)

type File struct {
	XMLName xml.Name `xml:"gpx"`
	Creator string   `xml:"creator,attr,omitempty"`
	Version string   `xml:"version,attr,omitempty"`
	Trk     []Track  `xml:"trk"`
	Rte     []Route  `xml:"rte"`
	Wpt     []Wpt    `xml:"wpt"`
}

type Track struct {
	Name string     `xml:"name"`
	Seg  []TrackSeg `xml:"trkseg"`
}

type TrackSeg struct {
	Pt []Wpt `xml:"trkpt"`
}

type Route struct {
	Name string `xml:"name"`
	Pt   []Wpt  `xml:"rtept"`
}

type Wpt struct {
	Lat  float64 `xml:"lat,attr"`
	Lon  float64 `xml:"lon,attr"`
	Ele  float64 `xml:"ele,omitempty"`
	Time string  `xml:"time,omitempty"`
}

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Ele float64 `json:"ele,omitempty"`
}

func Load(path string) ([]Point, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := xml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse gpx: %w", err)
	}
	var points []Point
	for _, trk := range f.Trk {
		for _, seg := range trk.Seg {
			for _, pt := range seg.Pt {
				points = append(points, Point{Lat: pt.Lat, Lon: pt.Lon, Ele: pt.Ele})
			}
		}
	}
	if len(points) == 0 {
		for _, rte := range f.Rte {
			for _, pt := range rte.Pt {
				points = append(points, Point{Lat: pt.Lat, Lon: pt.Lon, Ele: pt.Ele})
			}
		}
	}
	if len(points) == 0 {
		for _, pt := range f.Wpt {
			points = append(points, Point{Lat: pt.Lat, Lon: pt.Lon, Ele: pt.Ele})
		}
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("no route points found in gpx")
	}
	return points, nil
}

func WriteTrack(path string, pts []Point) error {
	g := File{
		Version: "1.1",
		Creator: "bgps",
		Trk: []Track{{
			Name: "Recorded Track",
			Seg:  []TrackSeg{{}},
		}},
	}
	for _, pt := range pts {
		g.Trk[0].Seg[0].Pt = append(g.Trk[0].Seg[0].Pt, Wpt{Lat: pt.Lat, Lon: pt.Lon, Ele: pt.Ele})
	}
	data, err := xml.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	return os.WriteFile(path, data, 0o644)
}
