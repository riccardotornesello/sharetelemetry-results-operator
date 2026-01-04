package main

import (
	"context"
	"flag"
	"log"
	"strconv"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"riccardotornesello.it/sharetelemetry/results/operator/pkg/competition"
)

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
	parsedEventGroups := make([]competition.EventGroup, 0, len(eventGroups))
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

		parsedEventGroups = append(parsedEventGroups, competition.EventGroup{
			TrackID:  trackID,
			FromTime: fromTime,
			ToTime:   toTime,
		})
	}

	// Build competition from command line arguments
	comp := competition.Competition{
		CompetitionID: competitionID,
		LeagueID:      leagueID,
		SeasonID:      seasonID,
		EventGroups:   parsedEventGroups,
	}

	// Process the competition
	if err := competition.Process(context.Background(), comp); err != nil {
		log.Fatalf("Failed to process competition: %v", err)
	}
}
