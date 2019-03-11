package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
	"github.com/golang/glog"
	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"github.com/twpayne/go-kml"
	"googlemaps.github.io/maps"
	"image/color"
	"io/ioutil"
	"math"
	"time"
)

const (
	maxElements = 25
	earthRadius = 6378137
	workers     = 20
)

type Result struct {
	Center, A, C s2.LatLng
	Duration     time.Duration
}

type ResultContainer struct {
	AreaStart, AreaEnd s2.LatLng
	Results            []Result
}

func FetchResults(apiKey string, dest, areaStart, areaEnd s2.LatLng, stepMeters int, opts Options) error {
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
				results, err := getResults(client, origins, dest, opts, stepLat, stepLon)
				if err != nil {
					glog.Fatalf("failed to get results: %v", err)
				}

				resultsCh <- results
			}
		}()
	}

	container := ResultContainer{AreaStart: areaStart, AreaEnd: areaEnd}
	for i := 0; i < len(origins); i += maxElements {
		container.Results = append(container.Results, (<-resultsCh)...)
		glog.Infof("%d/%d origins fetched", i, len(origins))
	}

	data, err := json.Marshal(container)
	if err != nil {
		return errors.Wrap(err, "faled to marshal json")
	}

	fmt.Printf("\n\n\n%s\n\n", data)

	return nil
}

func RenderKml(jsonFile string, maxDuration time.Duration, grades int) error {
	data, err := ioutil.ReadFile(jsonFile)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("faled to read file %q", jsonFile))
	}

	var container ResultContainer

	err = json.Unmarshal(data, &container)
	if err != nil {
		return errors.Wrap(err, "faled to unmarshal json")
	}

	renderedKml, err := getKml(container.Results, container.AreaStart, container.AreaEnd, maxDuration, grades)
	if err != nil {
		return errors.Wrap(err, "faled to get KML")
	}

	fmt.Printf("\n\n\n%s\n\n", renderedKml)

	return nil
}

func getResults(client *maps.Client, origins []s2.LatLng, dest s2.LatLng, opts Options, stepLat, stepLon s1.Angle) ([]Result, error) {
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

		a, c := getOriginBounds(origins[i], stepLat, stepLon)
		result := Result{
			Center:   origins[i],
			A:        a,
			C:        c,
			Duration: duration,
		}

		results = append(results, result)
	}

	return results, nil
}

func getKml(results []Result, areaStart, areaEnd s2.LatLng, maxDuration time.Duration, grades int) ([]byte, error) {
	grades = 6
	styles := []*kml.SharedElement{
		kml.SharedStyle("zone-denied", kml.PolyStyle(kml.Color(color.RGBA{})), kml.LineStyle(kml.Width(0))),
		kml.SharedStyle("zone-0", kml.PolyStyle(kml.Color(color.RGBA{A: 0x70, R: 0x00, G: 0xFF})), kml.LineStyle(kml.Width(0))),
		kml.SharedStyle("zone-1", kml.PolyStyle(kml.Color(color.RGBA{A: 0x70, R: 0x88, G: 0xFF})), kml.LineStyle(kml.Width(0))),
		kml.SharedStyle("zone-2", kml.PolyStyle(kml.Color(color.RGBA{A: 0x70, R: 0xFF, G: 0xFF})), kml.LineStyle(kml.Width(0))),
		kml.SharedStyle("zone-3", kml.PolyStyle(kml.Color(color.RGBA{A: 0x70, R: 0xFF, G: 0xAA})), kml.LineStyle(kml.Width(0))),
		kml.SharedStyle("zone-4", kml.PolyStyle(kml.Color(color.RGBA{A: 0x70, R: 0xFF, G: 0x55})), kml.LineStyle(kml.Width(0))),
		kml.SharedStyle("zone-5", kml.PolyStyle(kml.Color(color.RGBA{A: 0x70, R: 0xFF, G: 0x00})), kml.LineStyle(kml.Width(0))),
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
		folder.Add(
			kml.Placemark(
				kml.Name(fmt.Sprintf(fmt.Sprintf("%.0f min", result.Duration.Minutes()))),
				kml.StyleURL(getStyleId(result.Duration, maxDuration, grades)),
				getPoly(result.A, result.C),
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
