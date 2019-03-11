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
	"time"
	"math"
)

const (
	maxElements = 25
	earthRadius = 6378137
	workers     = 20
)

type Result struct {
	s2.LatLng
	Duration time.Duration
}

func Run(apiKey string, dest, areaStart, areaEnd s2.LatLng, stepMeters int, maxDuration time.Duration, opts Options) error {
	stepLat := s1.Angle(float64(stepMeters) / earthRadius)
	stepLon := s1.Angle(float64(stepMeters) / (earthRadius * math.Cos(float64(dest.Lat))))

	origins, err := getLatLngsInRect(areaStart, areaEnd, stepLat, stepLon)
	if err != nil {
		return errors.Wrap(err, "faled to get src points")
	}

	resultsCh := make(chan []Result)
	originsCh := make(chan []s2.LatLng)

	go func() {
		for i := 0; i < len(origins); i += maxElements {
			end := i + maxElements
			if end > len(origins) {
				end = len(origins)
			}

			originsCh <- origins[i:end]
		}

		close(originsCh)
	}()

	for i := 0; i < workers; i++ {
		go func() {
			client, err := maps.NewClient(maps.WithAPIKey(apiKey))
			if err != nil {
				glog.Fatalf("failed to create client: %v", err)
			}

			for origins := range originsCh {
				results, err := getResults(client, origins, dest, opts)
				if err != nil {
					glog.Fatalf("failed to get results: %v", err)
				}

				resultsCh <- results
			}
		}()
	}

	var results []Result
	for i := 0; i < len(origins); i += maxElements {
		results = append(results, (<-resultsCh)...)
		glog.Infof("%d/%d origins fetched", i, len(origins))
	}

	data, err := getKml(
		results,
		stepLat,
		stepLon,
		areaStart,
		areaEnd,
		maxDuration,
		3,
	)

	fmt.Printf("\n\n\n%s\n\n", data)

	return nil
}

func getResults(client *maps.Client, origins []s2.LatLng, dest s2.LatLng, opts Options) ([]Result, error) {
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

	if len(origins) != len(resp.Rows) {
		return nil, fmt.Errorf("len(origins) != len(resp.Rows): %d != %d ", len(origins), len(resp.Rows))
	}

	var results []Result

	for i, row := range resp.Rows {
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

		result := Result{
			LatLng:   origins[i],
			Duration: duration,
		}

		results = append(results, result)
	}

	return results, nil
}

func getKml(results []Result, stepLat, stepLon s1.Angle, areaStart, areaEnd s2.LatLng, maxDuration time.Duration, grades int, ) ([]byte, error) {
	grades = 3
	styles := []*kml.SharedElement{
		kml.SharedStyle("zone-denied", kml.PolyStyle(kml.Color(color.RGBA{})), kml.LineStyle(kml.Width(0), kml.Color(color.RGBA{A: 128}))),
		kml.SharedStyle("zone-0", kml.PolyStyle(kml.Color(color.RGBA{A: 80, R: 0, G: 0xFF})), kml.LineStyle(kml.Width(0), kml.Color(color.RGBA{A: 128, R: 0, G: 0xFF, B: 0})), ),
		kml.SharedStyle("zone-1", kml.PolyStyle(kml.Color(color.RGBA{A: 80, R: 0xFF, G: 0xFF})), kml.LineStyle(kml.Width(0), kml.Color(color.RGBA{A: 128, R: 0xFF, G: 0xFF, B: 0})), ),
		kml.SharedStyle("zone-2", kml.PolyStyle(kml.Color(color.RGBA{A: 80, R: 0xFF, G: 0x8C})), kml.LineStyle(kml.Width(0), kml.Color(color.RGBA{A: 128, R: 0xFF, G: 0, B: 0}))),
	}

	document := kml.Document()
	for _, v := range styles {
		document.Add(v)
	}

	// add boundaries
	folder := kml.Folder(
		kml.Placemark(
			kml.Style(kml.PolyStyle(kml.Color(color.RGBA{}))),
			getPoly(areaStart, areaEnd),
		),
	)

	for _, result := range results {
		a, c := getOriginBounds(result.LatLng, stepLat, stepLon)
		folder.Add(
			kml.Placemark(
				kml.Name(fmt.Sprintf(fmt.Sprintf("%.0f min", result.Duration.Minutes()))),
				kml.StyleURL(getStyleId(result.Duration, maxDuration, grades)),
				getPoly(a, c),
			),
		)
	}

	document.Add(folder)

	buf := new(bytes.Buffer)
	err := kml.KML(document).WriteIndent(buf, " ", " ")
	if err != nil {
		return nil, errors.Wrap(err, "failed to write KML")
	}

	return buf.Bytes(), nil
}

func getStyleId(duration, maxDuration time.Duration, grades int) string {
	if duration > maxDuration {
		return "#zone-denied"
	}
	grade := uint64(grades) * uint64(duration) / uint64(maxDuration)
	return fmt.Sprintf(`#zone-%d`, grade)
}

func getOriginBounds(origin s2.LatLng, stepLat, stepLon s1.Angle) (a, c s2.LatLng) {
	return s2.LatLng{Lat: origin.Lat + stepLat/2, Lng: origin.Lng - stepLon/2},
		s2.LatLng{Lat: origin.Lat - stepLat/2, Lng: origin.Lng + stepLon/2}
}

func getPoly(a, c s2.LatLng) *kml.CompoundElement {
	poly := kml.Polygon(
		kml.Extrude(true),
		kml.AltitudeMode("relativeToGround"),
		kml.OuterBoundaryIs(
			kml.LinearRing(
				kml.Coordinates(
					kml.Coordinate{Lat: a.Lat.Degrees(), Lon: a.Lng.Degrees()},
					kml.Coordinate{Lat: a.Lat.Degrees(), Lon: c.Lng.Degrees()},
					kml.Coordinate{Lat: c.Lat.Degrees(), Lon: c.Lng.Degrees()},
					kml.Coordinate{Lat: c.Lat.Degrees(), Lon: a.Lng.Degrees()},
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
}

func latLonToSt5ring(ll s2.LatLng) string {
	return fmt.Sprintf("%.6f,%.6f", ll.Lat.Degrees(), ll.Lng.Degrees())
}
