package sharetelemetryresultsoperator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
	"github.com/cloudevents/sdk-go/v2/event"

	"riccardotornesello.it/sharetelemetry/results/operator/pkg/competition"
)

// MessagePublishedData is the payload of a Pub/Sub event.
type MessagePublishedData struct {
	Message struct {
		Data []byte `json:"data"`
	} `json:"message"`
}

func init() {
	functions.CloudEvent("ProcessCompetition", ProcessCompetition)
}

// ProcessCompetition is the entry point for the Cloud Run function.
// It processes competition results triggered by a Pub/Sub message.
func ProcessCompetition(ctx context.Context, e event.Event) error {
	// Parse the Pub/Sub message
	var msg MessagePublishedData
	if err := e.DataAs(&msg); err != nil {
		return fmt.Errorf("event.DataAs: %w", err)
	}

	var comp competition.Competition
	err := json.Unmarshal(msg.Message.Data, &comp)
	if err != nil {
		return fmt.Errorf("json.Unmarshal: %w", err)
	}

	log.Printf("Processing competition %d for league %d season %d", comp.CompetitionID, comp.LeagueID, comp.SeasonID)

	// Process the competition
	if err := competition.Process(ctx, comp); err != nil {
		return fmt.Errorf("failed to process competition: %w", err)
	}

	log.Printf("Successfully processed competition %d", comp.CompetitionID)
	return nil
}
