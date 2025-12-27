package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"riccardotornesello.it/sharetelemetry/results/operator/pkg/processing"

	"github.com/riccardotornesello/irapi-go/pkg/api/results/lap_data"
	"github.com/riccardotornesello/sharetelemetry-iracing-scraper/pkg/database"
	scraperprocessing "github.com/riccardotornesello/sharetelemetry-iracing-scraper/pkg/processing"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Result struct {
	SubsessionID     int64            `bson:"subsession_id"`
	DriverID         int64            `bson:"driver_id"`
	AverageLapTimeMs int64            `bson:"average_lap_time_ms"`
	Laps             []processing.Lap `bson:"laps"`
}

type Competition struct {
	CompetitionID int64 `bson:"competition_id"`

	LeagueID int64 `bson:"league_id"`
	SeasonID int64 `bson:"season_id"`

	EventGroups []EventGroup `bson:"event_groups"`
}

type EventGroup struct {
	TrackID  int64
	FromTime time.Time
	ToTime   time.Time
}

func main() {
	// TODO: get details from call arguments
	competition := Competition{
		CompetitionID: 12345,
		LeagueID:      7843,
		SeasonID:      0,
		EventGroups: []EventGroup{
			{
				TrackID:  498,
				FromTime: time.Date(2025, 5, 9, 11, 0, 0, 0, time.UTC),
				ToTime:   time.Date(2025, 5, 9, 23, 0, 0, 0, time.UTC),
			},
			{
				TrackID:  498,
				FromTime: time.Date(2025, 5, 10, 11, 0, 0, 0, time.UTC),
				ToTime:   time.Date(2025, 5, 10, 23, 0, 0, 0, time.UTC),
			},
			{
				TrackID:  498,
				FromTime: time.Date(2025, 5, 11, 11, 0, 0, 0, time.UTC),
				ToTime:   time.Date(2025, 5, 11, 23, 0, 0, 0, time.UTC),
			},
		},
	}

	dbUri := os.Getenv("MONGODB_URI")
	scraperDbName := os.Getenv("MONGODB_SCRAPER_DATABASE")
	operatorDbName := os.Getenv("MONGODB_OPERATOR_DATABASE")

	scraperDb := database.Connect(dbUri, scraperDbName)
	defer scraperDb.Disconnect()
	operatorDb := database.Connect(dbUri, operatorDbName)
	defer operatorDb.Disconnect()

	// Get the season
	season := scraperprocessing.SeasonDoc{}
	err := scraperDb.DB.Collection("seasons").FindOne(scraperDb.Ctx, map[string]interface{}{
		"meta.kind": "iracing_league_season",
		"meta.name": fmt.Sprintf("league_%d_season_%d", competition.LeagueID, competition.SeasonID),
	}).Decode(&season)
	if err != nil {
		panic(err)
	}

	// Get which parsed sessions to process
	sessionsToProcess := []int64{}
	for subsessionIdStr, sessionStatus := range season.Status.ParsedSessions {
		subsessionId, err := strconv.ParseInt(subsessionIdStr, 10, 64)
		if err != nil {
			panic(err)
		}

		if sessionInEventGroups(&sessionStatus, competition.EventGroups) {
			sessionsToProcess = append(sessionsToProcess, subsessionId)
		}
	}

	log.Printf("Sessions to process: %d\n", len(sessionsToProcess))

	// Get all the laps documents for the sessions to process
	rawLapsDocs, err := scraperDb.DB.Collection(scraperprocessing.SessionCollection).Find(scraperDb.Ctx, map[string]interface{}{
		"meta.kind":                     scraperprocessing.LapsKind,
		"meta.labels.subsession_id":     map[string]interface{}{"$in": sessionsToProcess},
		"meta.labels.simsession_number": 0,
	})
	if err != nil {
		panic(err)
	}
	defer rawLapsDocs.Close(scraperDb.Ctx)

	var lapsDocs []scraperprocessing.LapsDoc
	if err := rawLapsDocs.All(scraperDb.Ctx, &lapsDocs); err != nil {
		panic(err)
	}

	log.Printf("Found %d laps documents\n", len(lapsDocs))

	// Group per session and driver
	results := make([]Result, len(lapsDocs))
	for i, lapsDoc := range lapsDocs {
		// Each document is for a single driver in a single session
		subsessionId := lapsDoc.Meta.Labels["subsession_id"].(int64)
		driverId := lapsDoc.Meta.Labels["cust_id"].(int64)

		// Parse the laps from the document to the iRacing structs
		rawLaps := []lap_data.ResultsLapDataResponseChunk{}
		dataBytes, err := json.Marshal(lapsDoc.Spec.Chunks)
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(dataBytes, &rawLaps); err != nil {
			panic(err)
		}

		// Convert to our Lap struct
		parsedLaps, err := processing.ParseLaps(rawLaps)
		if err != nil {
			panic(err)
		}

		// Calculate average lap time for the driver in this session
		averageLapTimeMs := processing.GetAverageTimeMs(parsedLaps, 3)

		results[i] = Result{
			SubsessionID:     subsessionId,
			DriverID:         driverId,
			AverageLapTimeMs: averageLapTimeMs,
			Laps:             parsedLaps,
		}
	}

	// Store the event document
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

	log.Printf("Stored event document with %d results\n", len(results))
}

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
