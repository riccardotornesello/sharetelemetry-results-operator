package competition

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

// Result represents the processed results for a single driver in a single session.
type Result struct {
	SubsessionID     int64            `json:"subsession_id" bson:"subsession_id"`             // iRacing subsession identifier
	DriverID         int64            `json:"driver_id" bson:"driver_id"`                     // iRacing customer/driver ID
	AverageLapTimeMs int64            `json:"average_lap_time_ms" bson:"average_lap_time_ms"` // Average lap time in milliseconds
	Laps             []processing.Lap `json:"laps" bson:"laps"`                               // All laps driven by this driver
}

// Competition defines a racing competition with its associated event groups.
type Competition struct {
	CompetitionID int64        `json:"competition_id" bson:"competition_id"` // Unique identifier for this competition
	LeagueID      int64        `json:"league_id" bson:"league_id"`           // iRacing league ID
	SeasonID      int64        `json:"season_id" bson:"season_id"`           // iRacing season ID (0 for current season)
	EventGroups   []EventGroup `json:"event_groups" bson:"event_groups"`     // Groups of events that are part of this competition
}

// EventGroup defines a set of racing events on a specific track within a time window.
type EventGroup struct {
	TrackID  int64     `json:"track_id" bson:"track_id"`   // iRacing track ID
	FromTime time.Time `json:"from_time" bson:"from_time"` // Start of the event window (inclusive)
	ToTime   time.Time `json:"to_time" bson:"to_time"`     // End of the event window (inclusive)
}

// Process handles the main logic for processing a competition's results.
// It retrieves session data, processes laps, calculates averages, and stores results in MongoDB.
func Process(ctx context.Context, competition Competition) error {
	// Get database configuration from environment variables
	dbUri := os.Getenv("MONGODB_URI")
	scraperDbName := os.Getenv("MONGODB_SCRAPER_DATABASE")
	operatorDbName := os.Getenv("MONGODB_OPERATOR_DATABASE")

	// Connect to databases
	scraperDb := database.Connect(dbUri, scraperDbName)
	defer scraperDb.Disconnect()
	operatorDb := database.Connect(dbUri, operatorDbName)
	defer operatorDb.Disconnect()

	log.Printf("Processing competition %d for league %d season %d",
		competition.CompetitionID, competition.LeagueID, competition.SeasonID)

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
		"meta.labels.simsession_number": 0, // 0 = last session (qualifying or race)
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
