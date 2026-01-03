# ShareTelemetry Results Operator

A Go-based operator for processing and calculating racing competition results from iRacing telemetry data. This tool retrieves race session data from MongoDB, processes lap times, and generates competition results.

## Features

- Processes iRacing league season data
- Calculates average lap times for drivers across multiple race sessions
- Filters sessions based on track and time constraints
- Stores processed results in MongoDB
- Can be deployed as a CLI tool or Google Cloud Run function

## Prerequisites

- Go 1.25.5 or higher
- MongoDB instance with iRacing scraper data
- Access to iRacing API (via irapi-go library)

## Installation

### From Source

```bash
go build -o operator ./cmd/operator
```

### Using Docker

```bash
docker build -t sharetelemetry-results-operator .
docker run -e MONGODB_URI=<your-uri> sharetelemetry-results-operator
```

## Configuration

The application requires the following environment variables:

- `MONGODB_URI`: MongoDB connection URI
- `MONGODB_SCRAPER_DATABASE`: Database name containing scraped iRacing data
- `MONGODB_OPERATOR_DATABASE`: Database name for storing processed results

You can use a `.env` file for local development:

```env
MONGODB_URI=mongodb://localhost:27017
MONGODB_SCRAPER_DATABASE=iracing_scraper
MONGODB_OPERATOR_DATABASE=iracing_results
```

## Usage

### Command Line

Run the operator with competition details as command line arguments:

```bash
./operator \
  --competition-id 12345 \
  --league-id 7843 \
  --season-id 0 \
  --event-group "498,2025-05-09T11:00:00Z,2025-05-09T23:00:00Z" \
  --event-group "498,2025-05-10T11:00:00Z,2025-05-10T23:00:00Z"
```

#### Arguments

- `--competition-id`: Unique identifier for the competition
- `--league-id`: iRacing league ID
- `--season-id`: iRacing season ID (use 0 for current season)
- `--event-group`: Event group definition in format "trackID,fromTime,toTime" (can be specified multiple times)

### Google Cloud Run Function

Deploy as a Cloud Run function that processes competitions triggered by Pub/Sub messages:

```bash
gcloud functions deploy process-competition \
  --runtime go125 \
  --trigger-topic competition-events \
  --entry-point ProcessCompetition \
  --set-env-vars MONGODB_URI=<your-uri>
```

The Pub/Sub message should contain a JSON payload with the competition details:

```json
{
  "competition_id": 12345,
  "league_id": 7843,
  "season_id": 0,
  "event_groups": [
    {
      "track_id": 498,
      "from_time": "2025-05-09T11:00:00Z",
      "to_time": "2025-05-09T23:00:00Z"
    }
  ]
}
```

## Architecture

The operator consists of three main components:

1. **Main Operator** (`cmd/operator/main.go`): CLI tool that processes competitions
2. **Cloud Function** (`function.go`): Google Cloud Run function for Pub/Sub triggers
3. **Processing Package** (`pkg/processing/`): Core logic for lap and stint calculations

### Data Flow

1. Retrieve season data from MongoDB (scraped by sharetelemetry-iracing-scraper)
2. Filter sessions based on event group criteria (track ID, time range)
3. Fetch lap data for each qualifying session
4. Parse and validate laps (excluding invalid laps with incidents or pit stops)
5. Calculate average lap times for each driver
6. Store results back to MongoDB

## Development

### Building

```bash
go build ./cmd/operator
```

### Running Tests

```bash
go test ./...
```

### Code Structure

```
.
├── cmd/
│   └── operator/          # CLI application entry point
│       └── main.go
├── pkg/
│   └── processing/        # Core processing logic
│       ├── laps.go        # Lap parsing and validation
│       └── stint.go       # Average time calculations
├── function.go            # Cloud Run function handler
├── Dockerfile             # Container image definition
└── go.mod                 # Go module dependencies
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Related Projects

- [sharetelemetry-iracing-scraper](https://github.com/riccardotornesello/sharetelemetry-iracing-scraper): Data scraper for iRacing telemetry
- [irapi-go](https://github.com/riccardotornesello/irapi-go): Go client library for iRacing API
