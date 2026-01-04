package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"riccardotornesello.it/sharetelemetry/results/operator/pkg/processing"

	"github.com/riccardotornesello/irapi-go/pkg/api/results/lap_data"
	"github.com/riccardotornesello/sharetelemetry-iracing-scraper/pkg/database"
	scraperprocessing "github.com/riccardotornesello/sharetelemetry-iracing-scraper/pkg/processing"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Result represents the processed results for a single driver in a single session.
type Result struct {
	SubsessionID     int64            `bson:"subsession_id"`       // iRacing subsession identifier
	DriverID         int64            `bson:"driver_id"`           // iRacing customer/driver ID
	AverageLapTimeMs int64            `bson:"average_lap_time_ms"` // Average lap time in milliseconds
	Laps             []processing.Lap `bson:"laps"`                // All laps driven by this driver
}

// Competition defines a racing competition with its associated event groups.
type Competition struct {
	CompetitionID int64 `bson:"competition_id"` // Unique identifier for this competition

	LeagueID int64 `bson:"league_id"` // iRacing league ID
	SeasonID int64 `bson:"season_id"` // iRacing season ID (0 for no season)

	EventGroups []EventGroup `bson:"event_groups"` // Groups of events that are part of this competition
}

// EventGroup defines a set of racing events on a specific track within a time window.
type EventGroup struct {
	TrackID  int64     // iRacing track ID
	FromTime time.Time // Start of the event window (inclusive)
	ToTime   time.Time // End of the event window (inclusive)
}

// eventGroupFlag is a custom flag type for parsing event group command line arguments.
type eventGroupFlag []string

func (e *eventGroupFlag) String() string {
	return strings.Join(*e, ",")
}

func (e *eventGroupFlag) Set(value string) error {
	*e = append(*e, value)
	return nil
}

func main() {
	// Parse command line flags
	var (
		competitionID int64
		leagueID      int64
		seasonID      int64
		eventGroups   eventGroupFlag
	)

	flag.Int64Var(&competitionID, "competition-id", 0, "Unique identifier for the competition")
	flag.Int64Var(&leagueID, "league-id", 0, "iRacing league ID")
	flag.Int64Var(&seasonID, "season-id", 0, "iRacing season ID (use 0 for current season)")
	flag.Var(&eventGroups, "event-group", "Event group in format 'trackID,fromTime,toTime' (can be specified multiple times)")
	flag.Parse()

	// Validate required flags
	if competitionID == 0 {
		log.Fatal("--competition-id is required")
	}
	if leagueID == 0 {
		log.Fatal("--league-id is required")
	}
	if len(eventGroups) == 0 {
		log.Fatal("at least one --event-group is required")
	}

	// Parse event groups from command line arguments
	parsedEventGroups := make([]EventGroup, 0, len(eventGroups))
	for _, eg := range eventGroups {
		parts := strings.Split(eg, ",")
		if len(parts) != 3 {
			log.Fatalf("Invalid event group format '%s'. Expected: trackID,fromTime,toTime", eg)
		}

		trackID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			log.Fatalf("Invalid track ID '%s': %v", parts[0], err)
		}

		fromTime, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			log.Fatalf("Invalid from time '%s': %v", parts[1], err)
		}

		toTime, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			log.Fatalf("Invalid to time '%s': %v", parts[2], err)
		}

		parsedEventGroups = append(parsedEventGroups, EventGroup{
			TrackID:  trackID,
			FromTime: fromTime,
			ToTime:   toTime,
		})
	}

	// Build competition from command line arguments
	competition := Competition{
		CompetitionID: competitionID,
		LeagueID:      leagueID,
		SeasonID:      seasonID,
		EventGroups:   parsedEventGroups,
	}

	// Process the competition
	processCompetition(competition)
}

// processCompetition handles the main logic for processing a competition's results.
func processCompetition(competition Competition) {
	// Get database configuration from environment variables
	dbUri := os.Getenv("MONGODB_URI")
	scraperDbName := os.Getenv("MONGODB_SCRAPER_DATABASE")
	operatorDbName := os.Getenv("MONGODB_OPERATOR_DATABASE")

	// Connect to databases
	scraperDb := database.Connect(dbUri, scraperDbName)
	defer scraperDb.Disconnect()
	operatorDb := database.Connect(dbUri, operatorDbName)
	defer operatorDb.Disconnect()

	log.Printf("Processing competition %d for league %d season %d\n",
		competition.CompetitionID, competition.LeagueID, competition.SeasonID)

	// Retrieve the season document from the scraper database
	season := scraperprocessing.SeasonDoc{}
	err := scraperDb.DB.Collection("seasons").FindOne(scraperDb.Ctx, map[string]interface{}{
		"meta.kind": "iracing_league_season",
		"meta.name": fmt.Sprintf("league_%d_season_%d", competition.LeagueID, competition.SeasonID),
	}).Decode(&season)
	if err != nil {
		panic(err)
	}

	// Filter sessions that match the event group criteria (track and time window)
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

	// Retrieve lap data for all sessions in the competition
	rawLapsDocs, err := scraperDb.DB.Collection(scraperprocessing.SessionCollection).Find(scraperDb.Ctx, map[string]interface{}{
		"meta.kind":                     scraperprocessing.LapsKind,
		"meta.labels.subsession_id":     map[string]interface{}{"$in": sessionsToProcess},
		"meta.labels.simsession_number": 0, // 0 = last session (qualify or race)
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
			panic(err)
		}
		if err := json.Unmarshal(dataBytes, &rawLaps); err != nil {
			panic(err)
		}

		// Convert the raw iRacing lap data to our internal Lap structure
		parsedLaps, err := processing.ParseLaps(rawLaps)
		if err != nil {
			panic(err)
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
		panic(err)
	}

	log.Printf("Successfully stored competition document with %d results\n", len(results))
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
