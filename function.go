package sharetelemetryresultsoperator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/riccardotornesello/irapi-go/pkg/api/results/lap_data"
	"github.com/riccardotornesello/sharetelemetry-iracing-scraper/pkg/database"
	scraperprocessing "github.com/riccardotornesello/sharetelemetry-iracing-scraper/pkg/processing"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"riccardotornesello.it/sharetelemetry/results/operator/pkg/processing"
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

// Result represents the processed results for a single driver in a single session.
type Result struct {
	SubsessionID     int64            `bson:"subsession_id"`
	DriverID         int64            `bson:"driver_id"`
	AverageLapTimeMs int64            `bson:"average_lap_time_ms"`
	Laps             []processing.Lap `bson:"laps"`
}

// Competition defines a racing competition with its associated event groups.
type Competition struct {
	CompetitionID int64        `bson:"competition_id"`
	LeagueID      int64        `bson:"league_id"`
	SeasonID      int64        `bson:"season_id"`
	EventGroups   []EventGroup `bson:"event_groups"`
}

// EventGroup defines a set of racing events on a specific track within a time window.
type EventGroup struct {
	TrackID  int64
	FromTime time.Time
	ToTime   time.Time
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
	eventGroups := make([]EventGroup, 0, len(msg.EventGroups))
	for _, eg := range msg.EventGroups {
		fromTime, err := time.Parse(time.RFC3339, eg.FromTime)
		if err != nil {
			return fmt.Errorf("failed to parse from_time: %w", err)
		}

		toTime, err := time.Parse(time.RFC3339, eg.ToTime)
		if err != nil {
			return fmt.Errorf("failed to parse to_time: %w", err)
		}

		eventGroups = append(eventGroups, EventGroup{
			TrackID:  eg.TrackID,
			FromTime: fromTime,
			ToTime:   toTime,
		})
	}

	competition := Competition{
		CompetitionID: msg.CompetitionID,
		LeagueID:      msg.LeagueID,
		SeasonID:      msg.SeasonID,
		EventGroups:   eventGroups,
	}

	// Process the competition
	if err := processCompetition(ctx, competition); err != nil {
		return fmt.Errorf("failed to process competition: %w", err)
	}

	log.Printf("Successfully processed competition %d", msg.CompetitionID)
	return nil
}

// processCompetition handles the main logic for processing a competition's results.
func processCompetition(ctx context.Context, competition Competition) error {
	// Get database configuration from environment variables
	dbUri := os.Getenv("MONGODB_URI")
	scraperDbName := os.Getenv("MONGODB_SCRAPER_DATABASE")
	operatorDbName := os.Getenv("MONGODB_OPERATOR_DATABASE")

	// Connect to databases
	scraperDb := database.Connect(dbUri, scraperDbName)
	defer scraperDb.Disconnect()
	operatorDb := database.Connect(dbUri, operatorDbName)
	defer operatorDb.Disconnect()

	// Retrieve the season document from the scraper database
	season := scraperprocessing.SeasonDoc{}
	err := scraperDb.DB.Collection("seasons").FindOne(scraperDb.Ctx, map[string]interface{}{
		"meta.kind": "iracing_league_season",
		"meta.name": fmt.Sprintf("league_%d_season_%d", competition.LeagueID, competition.SeasonID),
	}).Decode(&season)
	if err != nil {
		return fmt.Errorf("failed to find season: %w", err)
	}

	// Filter sessions that match the event group criteria (track and time window)
	sessionsToProcess := []int64{}
	for subsessionIdStr, sessionStatus := range season.Status.ParsedSessions {
		subsessionId, err := strconv.ParseInt(subsessionIdStr, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse subsession ID: %w", err)
		}

		if sessionInEventGroups(&sessionStatus, competition.EventGroups) {
			sessionsToProcess = append(sessionsToProcess, subsessionId)
		}
	}

	log.Printf("Sessions to process: %d", len(sessionsToProcess))

	// Retrieve lap data for all sessions in the competition
	rawLapsDocs, err := scraperDb.DB.Collection(scraperprocessing.SessionCollection).Find(scraperDb.Ctx, map[string]interface{}{
		"meta.kind":                     scraperprocessing.LapsKind,
		"meta.labels.subsession_id":     map[string]interface{}{"$in": sessionsToProcess},
		"meta.labels.simsession_number": 0, // 0 = race session
	})
	if err != nil {
		return fmt.Errorf("failed to find laps: %w", err)
	}
	defer rawLapsDocs.Close(scraperDb.Ctx)

	var lapsDocs []scraperprocessing.LapsDoc
	if err := rawLapsDocs.All(scraperDb.Ctx, &lapsDocs); err != nil {
		return fmt.Errorf("failed to decode laps: %w", err)
	}

	log.Printf("Found %d laps documents", len(lapsDocs))

	// Process each driver's laps and calculate results
	results := make([]Result, len(lapsDocs))
	for i, lapsDoc := range lapsDocs {
		// Each document is for a single driver in a single session
		subsessionId := lapsDoc.Meta.Labels["subsession_id"].(int64)
		driverId := lapsDoc.Meta.Labels["cust_id"].(int64)

		// Parse the laps from the document to the iRacing structs
		rawLaps := []lap_data.ResultsLapDataResponseChunk{}
		dataBytes, err := json.Marshal(lapsDoc.Spec.Chunks)
		if err != nil {
			return fmt.Errorf("failed to marshal laps: %w", err)
		}
		if err := json.Unmarshal(dataBytes, &rawLaps); err != nil {
			return fmt.Errorf("failed to unmarshal laps: %w", err)
		}

		// Convert the raw iRacing lap data to our internal Lap structure
		parsedLaps, err := processing.ParseLaps(rawLaps)
		if err != nil {
			return fmt.Errorf("failed to parse laps: %w", err)
		}

		// Calculate average lap time using the first 3 valid consecutive laps
		averageLapTimeMs := processing.GetAverageTimeMs(parsedLaps, 3)

		results[i] = Result{
			SubsessionID:     subsessionId,
			DriverID:         driverId,
			AverageLapTimeMs: averageLapTimeMs,
			Laps:             parsedLaps,
		}
	}

	// Store the competition results in the operator database
	_, err = operatorDb.DB.Collection("competitions").UpdateOne(
		operatorDb.Ctx,
		map[string]interface{}{
			"competition_id": competition.CompetitionID,
		},
		map[string]interface{}{
			"$set": map[string]interface{}{
				"results": results,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to store results: %w", err)
	}

	log.Printf("Successfully stored competition document with %d results", len(results))
	return nil
}

// sessionInEventGroups checks if a session matches any of the event group criteria.
// A session matches if it's on the correct track and within the time window of an event group.
func sessionInEventGroups(session *scraperprocessing.SeasonStatusSession, eventGroups []EventGroup) bool {
	if session.TrackID == nil || session.LaunchAt == nil {
		return false
	}

	for _, eventGroup := range eventGroups {
		if *session.TrackID != eventGroup.TrackID {
			continue
		}

		if session.LaunchAt.Before(eventGroup.FromTime) {
			continue
		}

		if session.LaunchAt.After(eventGroup.ToTime) {
			continue
		}

		return true
	}

	return false
}
