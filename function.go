package sharetelemetryresultsoperator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"riccardotornesello.it/sharetelemetry/results/operator/pkg/competition"
)

// PubSubMessage is the payload of a Pub/Sub event.
type PubSubMessage struct {
	Data []byte `json:"data"`
}

// CompetitionMessage represents the competition details from a Pub/Sub message.
type CompetitionMessage struct {
	CompetitionID int64               `json:"competition_id"`
	LeagueID      int64               `json:"league_id"`
	SeasonID      int64               `json:"season_id"`
	EventGroups   []EventGroupMessage `json:"event_groups"`
}

// EventGroupMessage represents an event group from the Pub/Sub message.
type EventGroupMessage struct {
	TrackID  int64  `json:"track_id"`
	FromTime string `json:"from_time"` // RFC3339 format
	ToTime   string `json:"to_time"`   // RFC3339 format
}

// ProcessCompetition is the entry point for the Cloud Run function.
// It processes competition results triggered by a Pub/Sub message.
func ProcessCompetition(ctx context.Context, m PubSubMessage) error {
	// Parse the Pub/Sub message
	var msg CompetitionMessage
	if err := json.Unmarshal(m.Data, &msg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	log.Printf("Processing competition %d for league %d season %d",
		msg.CompetitionID, msg.LeagueID, msg.SeasonID)

	// Parse event groups
	eventGroups := make([]competition.EventGroup, 0, len(msg.EventGroups))
	for _, eg := range msg.EventGroups {
		fromTime, err := time.Parse(time.RFC3339, eg.FromTime)
		if err != nil {
			return fmt.Errorf("failed to parse from_time: %w", err)
		}

		toTime, err := time.Parse(time.RFC3339, eg.ToTime)
		if err != nil {
			return fmt.Errorf("failed to parse to_time: %w", err)
		}

		eventGroups = append(eventGroups, competition.EventGroup{
			TrackID:  eg.TrackID,
			FromTime: fromTime,
			ToTime:   toTime,
		})
	}

	comp := competition.Competition{
		CompetitionID: msg.CompetitionID,
		LeagueID:      msg.LeagueID,
		SeasonID:      msg.SeasonID,
		EventGroups:   eventGroups,
	}

	// Process the competition
	if err := competition.Process(ctx, comp); err != nil {
		return fmt.Errorf("failed to process competition: %w", err)
	}

	log.Printf("Successfully processed competition %d", msg.CompetitionID)
	return nil
}
