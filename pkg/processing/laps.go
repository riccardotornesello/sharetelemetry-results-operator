package processing

import (
	"fmt"

	"github.com/riccardotornesello/irapi-go/pkg/api/results/lap_data"
)

// Lap represents a single lap in a racing session with all relevant timing and event data.
// It contains information about lap performance, flags, incidents, and session timing.
type Lap struct {
	LapNumber   int64    `bson:"lap_number"`   // Lap number in the session (-1 for out lap)
	Flags       int64    `bson:"flags"`        // Flag conditions during the lap (bitfield)
	Incident    bool     `bson:"incident"`     // Whether an incident occurred during this lap
	SessionTime int64    `bson:"session_time"` // Session time in milliseconds when lap was completed
	LapTime     int64    `bson:"lap_time"`     // Lap time in 1/10000th of a second
	LapEvents   []string `bson:"lap_events"`   // Events that occurred during the lap (e.g., "pitted")
}

// ParseLaps converts iRacing API lap data chunks into our internal Lap representation.
// It validates that laps are in sequential order and returns an error if they are not sorted.
//
// Parameters:
//   - chunks: Array of lap data from the iRacing API
//
// Returns:
//   - []Lap: Parsed lap data in our internal format
//   - error: Error if laps are not sorted by lap number
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

		// Ensure laps are sorted by lap number for proper processing
		if i > 0 && laps[i].LapNumber <= laps[i-1].LapNumber {
			return nil, fmt.Errorf("laps not sorted")
		}
	}

	return laps, nil
}
