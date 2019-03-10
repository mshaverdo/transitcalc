package app

import (
	"bytes"
	"context"
	"fmt"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
	"github.com/golang/glog"
	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"github.com/twpayne/go-kml"
	"googlemaps.github.io/maps"
	"image/color"
	"math"
	"time"
)

const (
	maxElements = 25
)

func Run(apiKey string, dest, rectStart, rectEnd s2.LatLng, stepMeters int, maxDuration time.Duration, opts Options) error {
	client, err := maps.NewClient(maps.WithAPIKey(apiKey))
	if err != nil {
		return errors.Wrap(err, "faled to create client")
	}

	stepLat := s1.Angle(float64(stepMeters)/111337.0) * s1.Degree
	stepLon := s1.Angle(float64(stepMeters)/62794.0) * s1.Degree

	origins, err := getLatLngsInRect(rectStart, rectEnd, stepLat, stepLon)
	if err != nil {
		return errors.Wrap(err, "faled to get src points")
	}

	var rows []maps.DistanceMatrixElementsRow

	for i := 0; i < len(origins); i += maxElements {
		end := i + maxElements
		if end > len(origins) {
			end = len(origins)
		}

		rowsChunk, err := getRows(client, origins[i:end], dest, opts)
		if err != nil {
			return err
		}

		rows = append(rows, rowsChunk...)

		glog.Infof("%d/%d origins fetched", i, len(origins))
	}

	data, err := getKml(origins, rows, stepLat, stepLon, maxDuration, 3)

	fmt.Printf("\n\n\n%s\n\n", data)

	return nil
}

func getRows(client *maps.Client, origins []s2.LatLng, dest s2.LatLng, opts Options) ([]maps.DistanceMatrixElementsRow, error) {
	r := &maps.DistanceMatrixRequest{
		Destinations: []string{latLonToSt5ring(dest)},
	}

	opts.Apply(r)

	for _, ll := range origins {
		r.Origins = append(r.Origins, latLonToSt5ring(ll))
	}

	resp, err := client.DistanceMatrix(context.Background(), r)
	//pretty.Println(resp)
	if err != nil {
		return nil, errors.Wrap(err, "faled to process request")
	}

	return resp.Rows, nil
}

func getKml(origins []s2.LatLng, rows []maps.DistanceMatrixElementsRow, stepLat, stepLon s1.Angle, maxDuration time.Duration, grades int, ) ([]byte, error) {
	if len(origins) != len(rows) {
		return nil, fmt.Errorf("len(origins) != len(resp.Rows): %d != %d ", len(origins), len(rows))
	}

	if len(origins) == 0 {
		return nil, nil
	}

	grades = 3
	styles := []*kml.SharedElement{
		kml.SharedStyle("zone0", kml.PolyStyle(kml.Color(color.RGBA{A: 80, R: 0, G: 0xFF, B: 0})), kml.LineStyle(kml.Width(0), kml.Color(color.RGBA{A: 128, R: 0, G: 0xFF, B: 0})), ),
		kml.SharedStyle("zone1", kml.PolyStyle(kml.Color(color.RGBA{A: 80, R: 0xFF, G: 0xFF, B: 0})), kml.LineStyle(kml.Width(0), kml.Color(color.RGBA{A: 128, R: 0xFF, G: 0xFF, B: 0})), ),
		kml.SharedStyle("zone2", kml.PolyStyle(kml.Color(color.RGBA{A: 80, R: 0xFF, G: 0x8C, B: 0})), kml.LineStyle(kml.Width(0), kml.Color(color.RGBA{A: 128, R: 0xFF, G: 0, B: 0}))),
	}

	document := kml.Document()
	for _, v := range styles {
		document.Add(v)
	}

	// add boundaries
	areaLat := (origins[0].Lat + origins[len(origins)-1].Lat) / 2
	areaLon := (origins[0].Lng + origins[len(origins)-1].Lng) / 2
	areaHeight := s1.Angle(math.Abs(float64(origins[0].Lat - origins[len(origins)-1].Lat)))
	areaWidth := s1.Angle(math.Abs(float64(origins[0].Lng - origins[len(origins)-1].Lng)))
	area := getPoly(s2.LatLng{Lat: areaLat, Lng: areaLon}, areaHeight, areaWidth)

	folder := kml.Folder(
		kml.Placemark(
			kml.Style(kml.PolyStyle(kml.Color(color.RGBA{A: 0, R: 0, G: 0, B: 0}))),
			area,
		),
	)

	for i, row := range rows {
		if len(row.Elements) != 1 {
			glog.Warning("Row elements != 1: ", pretty.Sprint(row))
			continue
		}
		if row.Elements[0].Status != "OK" {
			glog.Warning("Row status != OK: ", pretty.Sprint(row))
			continue
		}

		duration := row.Elements[0].Duration
		if row.Elements[0].DurationInTraffic > 0 {
			duration = row.Elements[0].DurationInTraffic
		}
		if duration > maxDuration {
			continue
		}

		folder.Add(
			kml.Placemark(
				kml.Name(fmt.Sprintf(fmt.Sprintf("%.0f min", duration.Minutes()))),
				kml.StyleURL(fmt.Sprintf(`#zone%d`, getStyleId(duration, maxDuration, grades))),
				getPoly(origins[i], stepLat, stepLon),
			),
		)
	}

	document.Add(folder)

	buf := new(bytes.Buffer)
	err := kml.KML(document).WriteIndent(buf, " ", " ")
	if err != nil {
		return nil, errors.Wrap(err, "faled to write KML")
	}

	return buf.Bytes(), nil
}

func getStyleId(duration, maxDuration time.Duration, grades int) int {
	grade := uint64(grades) * uint64(duration) / uint64(maxDuration)
	return int(grade)
}

func getPoly(origin s2.LatLng, stepLat, stepLon s1.Angle) *kml.CompoundElement {
	//stepLat *= 0.99
	a, b, c, d := origin, origin, origin, origin

	a.Lat -= stepLat / 2
	a.Lng += stepLon / 2

	b.Lat += stepLat / 2
	b.Lng += stepLon / 2

	c.Lat += stepLat / 2
	c.Lng -= stepLon / 2

	d.Lat -= stepLat / 2
	d.Lng -= stepLon / 2

	poly := kml.Polygon(
		kml.Extrude(true),
		kml.AltitudeMode("relativeToGround"),
		kml.OuterBoundaryIs(
			kml.LinearRing(
				kml.Coordinates(
					kml.Coordinate{Lat: a.Lat.Degrees(), Lon: a.Lng.Degrees()},
					kml.Coordinate{Lat: b.Lat.Degrees(), Lon: b.Lng.Degrees()},
					kml.Coordinate{Lat: c.Lat.Degrees(), Lon: c.Lng.Degrees()},
					kml.Coordinate{Lat: d.Lat.Degrees(), Lon: d.Lng.Degrees()},
				),
			),
		),
	)

	return poly
}

func getLatLngsInRect(rectStart, rectEnd s2.LatLng, stepLat, stepLon s1.Angle) (origins []s2.LatLng, err error) {
	if rectEnd.Lat < rectStart.Lat {
		rectEnd.Lat, rectStart.Lat = rectStart.Lat, rectEnd.Lat
	}
	if rectEnd.Lng < rectStart.Lng {
		rectEnd.Lng, rectStart.Lng = rectStart.Lng, rectEnd.Lng
	}

	for lat := rectStart.Lat; lat < rectEnd.Lat; lat += stepLat {
		for lon := rectStart.Lng; lon < rectEnd.Lng; lon += stepLon {
			origins = append(origins, s2.LatLng{Lat: lat, Lng: lon})
		}
	}

	return

	//return []s2.LatLng{
	//	s2.LatLngFromDegrees(55.795846, 37.403290),
	//	s2.LatLngFromDegrees(55.806238, 37.515073),
	//	s2.LatLngFromDegrees(55.742389, 37.488608),
	//}, nil
}

func latLonToSt5ring(ll s2.LatLng) string {
	return fmt.Sprintf("%.6f,%.6f", ll.Lat.Degrees(), ll.Lng.Degrees())
}
