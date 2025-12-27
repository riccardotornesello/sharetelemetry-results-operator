package processing

import (
	"fmt"

	"github.com/riccardotornesello/irapi-go/pkg/api/results/lap_data"
)

type Lap struct {
	LapNumber   int64    `bson:"lap_number"`
	Flags       int64    `bson:"flags"`
	Incident    bool     `bson:"incident"`
	SessionTime int64    `bson:"session_time"`
	LapTime     int64    `bson:"lap_time"`
	LapEvents   []string `bson:"lap_events"`
}

func ParseLaps(chunks []lap_data.ResultsLapDataResponseChunk) ([]Lap, error) {
	laps := make([]Lap, len(chunks))

	for i, lap := range chunks {
		laps[i] = Lap{
			LapNumber:   lap.LapNumber,
			Flags:       lap.Flags,
			Incident:    lap.Incident,
			SessionTime: lap.SessionTime,
			LapTime:     lap.LapTime,
			LapEvents:   lap.LapEvents,
		}

		if i > 0 && laps[i].LapNumber <= laps[i-1].LapNumber {
			return nil, fmt.Errorf("laps not sorted")
		}
	}

	return laps, nil
}
