package app

import (
	"fmt"
	"github.com/golang/glog"
	"googlemaps.github.io/maps"
	"strings"
	"time"
)

type Options struct {
	Mode                     string
	Language                 string
	Avoid                    string
	Units                    string
	DepartureTime            time.Time
	ArrivalTime              time.Time
	TransitMode              string
	TransitRoutingPreference string
	TrafficModel             string
}

func (o Options) Apply(r *maps.DistanceMatrixRequest) {
	r.DepartureTime = getTime(o.DepartureTime)
	r.ArrivalTime = getTime(o.ArrivalTime)
	r.Language = o.Language

	lookupMode(o.Mode, r)
	lookupAvoid(o.Avoid, r)
	lookupUnits(o.Units, r)
	lookupTransitMode(o.TransitMode, r)
	lookupTransitRoutingPreference(o.TransitRoutingPreference, r)
	lookupTrafficModel(o.TrafficModel, r)
}

func getTime(field time.Time) string {
	if field == (time.Time{}){
		return ""
	}

	return fmt.Sprintf("%d", field.Unix())
}

func lookupMode(mode string, r *maps.DistanceMatrixRequest) {
	switch mode {
	case "driving":
		r.Mode = maps.TravelModeDriving
	case "walking":
		r.Mode = maps.TravelModeWalking
	case "bicycling":
		r.Mode = maps.TravelModeBicycling
	case "transit":
		r.Mode = maps.TravelModeTransit
	case "":
		// ignore
	default:
		glog.Fatalf("Unknown mode %s", mode)
	}
}

func lookupAvoid(avoid string, r *maps.DistanceMatrixRequest) {
	switch avoid {
	case "tolls":
		r.Avoid = maps.AvoidTolls
	case "highways":
		r.Avoid = maps.AvoidHighways
	case "ferries":
		r.Avoid = maps.AvoidFerries
	case "":
		// ignore
	default:
		glog.Fatalf("Unknown avoid restriction %s", avoid)
	}
}

func lookupUnits(units string, r *maps.DistanceMatrixRequest) {
	switch units {
	case "metric":
		r.Units = maps.UnitsMetric
	case "imperial":
		r.Units = maps.UnitsImperial
	case "":
		// ignore
	default:
		glog.Fatalf("Unknown units %s", units)
	}
}

func lookupTransitMode(transitMode string, r *maps.DistanceMatrixRequest) {
	if transitMode != "" {
		for _, m := range strings.Split(transitMode, "|") {
			switch m {
			case "bus":
				r.TransitMode = append(r.TransitMode, maps.TransitModeBus)
			case "subway":
				r.TransitMode = append(r.TransitMode, maps.TransitModeSubway)
			case "train":
				r.TransitMode = append(r.TransitMode, maps.TransitModeTrain)
			case "tram":
				r.TransitMode = append(r.TransitMode, maps.TransitModeTram)
			case "rail":
				r.TransitMode = append(r.TransitMode, maps.TransitModeRail)
			default:
				glog.Fatalf("Unknown transit_mode %s", m)
			}
		}
	}
}

func lookupTransitRoutingPreference(transitRoutingPreference string, r *maps.DistanceMatrixRequest) {
	switch transitRoutingPreference {
	case "fewer_transfers":
		r.TransitRoutingPreference = maps.TransitRoutingPreferenceFewerTransfers
	case "less_walking":
		r.TransitRoutingPreference = maps.TransitRoutingPreferenceLessWalking
	case "":
		// ignore
	default:
		glog.Fatalf("Unknown transit routing preference %s", transitRoutingPreference)
	}
}

func lookupTrafficModel(trafficModel string, r *maps.DistanceMatrixRequest) {
	switch trafficModel {
	case "best_guess":
		r.TrafficModel = maps.TrafficModelBestGuess
	case "pessimistic":
		r.TrafficModel = maps.TrafficModelPessimistic
	case "optimistic":
		r.TrafficModel = maps.TrafficModelOptimistic
	case "":
		// ignore
	default:
		glog.Fatalf("Unknown traffic_model %s", trafficModel)
	}
}
