package main

import (
	"flag"
	"fmt"
	"github.com/golang/geo/s2"
	"github.com/golang/glog"
	"github.com/mshaverdo/transitcalc/cmd/app"
	"github.com/pkg/errors"
	"regexp"
	"strconv"
	"time"
)

const (
	timeLayout = "2006-01-02 15:04"
)

func main() {
	var apiKey, destStr, arrTimeStr, depTimeStr string
	var (
		maxDurationMins = 30
		stepMeters = 500
	)
	var opts app.Options

	flag.StringVar(&apiKey, "key", apiKey, "distance matrix google API key")
	flag.StringVar(&destStr, "dst", destStr, "destination coords")
	flag.IntVar(&maxDurationMins, "max_duratoin", maxDurationMins, "dmax duration")
	flag.IntVar(&stepMeters, "step", stepMeters, "step in meters")

	flag.StringVar(&depTimeStr, "departure_time", "", "The desired time of departure `"+timeLayout+"`.")
	flag.StringVar(&arrTimeStr, "arrival_time", "", "Specifies the desired time of arrival `"+timeLayout+"`.")
	flag.StringVar(&opts.Mode, "mode", "", "Specifies the mode of transport to use when calculating distance.")
	flag.StringVar(&opts.Language, "language", "", "The language in which to return results.")
	flag.StringVar(&opts.Avoid, "avoid", "", "Introduces restrictions to the route.")
	flag.StringVar(&opts.Units, "units", "", "Specifies the unit system to use when expressing distance as text.")
	flag.StringVar(&opts.TransitRoutingPreference, "transit_routing_preference", "", "Specifies preferences for transit requests.")
	flag.StringVar(&opts.TrafficModel, "traffic_model", "", "Specifies the assumptions to use when calculating time in traffic.")
	flag.StringVar(&opts.TransitMode, "transit_mode", "", "Specifies one or more preferred modes of transit.")

	flag.Parse()

	if flag.NArg() != 2 {
		glog.Fatal("No origin area specified")
	}

	rectStart, err := PointFromString(flag.Arg(0))
	CheckErr(err, "Invalid rectStart")
	rectEnd, err := PointFromString(flag.Arg(1))
	CheckErr(err, "Invalid rectEnd")

	if arrTimeStr != "" {
		opts.ArrivalTime, err = time.ParseInLocation(timeLayout, arrTimeStr, time.Now().Location())
		CheckErr(err, "Invalid arrivalTime")
	}

	if depTimeStr != "" {
		opts.DepartureTime, err = time.ParseInLocation(timeLayout, depTimeStr, time.Now().Location())
		CheckErr(err, "Invalid depTimeStr")
	}

	dest, err := PointFromString(destStr)
	CheckErr(err, "Invalid rectEnd")

	err = app.Run(
		apiKey,
		dest,
		rectStart,
		rectEnd,
		stepMeters,
		time.Duration(maxDurationMins)*time.Minute,
		opts,
	)
	CheckErr(err, "Failed to run app")
}

func CheckErr(err error, msg string) {
	if err != nil {
		glog.Fatal(msg, ": ", err)
	}
}

func PointFromString(s string) (ll s2.LatLng, err error) {
	re := regexp.MustCompile(`(\d+\.\d+),\s*(\d+\.\d+)`)
	parts := re.FindStringSubmatch(s)
	if len(parts) != 3 {
		return ll, fmt.Errorf("invalid point string: %q", s)
	}

	lat, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return ll, errors.Wrap(err, "invalid lat")
	}
	lon, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return ll, errors.Wrap(err, "invalid lon")
	}

	return s2.LatLngFromDegrees(lat, lon), nil
}
