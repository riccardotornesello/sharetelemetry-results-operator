package processing

// GetAverageTimeMs calculates the average lap time in milliseconds from a sequence of valid laps.
// It processes laps in order and stops after finding the required number of valid consecutive laps.
// The function returns 0 if it cannot find enough valid laps or if any lap in the sequence is invalid.
//
// Valid laps must:
//   - Not be out laps (lap number != -1)
//   - Not have pit stops
//   - Not have incidents
//   - Have positive lap times
//   - Have no lap events
//
// Parameters:
//   - laps: Array of laps to process, should be in sequential order
//   - requiredValidLaps: Number of consecutive valid laps needed for the average
//
// Returns:
//   - int64: Average lap time in milliseconds, or 0 if requirements not met
func GetAverageTimeMs(laps []Lap, requiredValidLaps int) int64 {
	var validLaps int = 0
	var totalLapTime int64 = 0

	for _, lap := range laps {
		// Skip out laps and pit laps, but reset if we already started counting
		if lap.LapNumber == -1 || isLapPitted(lap) {
			if validLaps > 0 {
				return 0
			}
			continue
		}

		// If any lap in the sequence is invalid, return 0
		if !isLapValid(lap) {
			return 0
		}

		validLaps += 1
		totalLapTime += lap.LapTime

		// Stop once we have enough valid laps
		if validLaps >= requiredValidLaps {
			break
		}
	}

	// Calculate average if we got exactly the required number of valid laps
	if validLaps == requiredValidLaps {
		// Convert from 1/10000th of a second to milliseconds
		totalLapTime /= (10 * int64(requiredValidLaps))
		return totalLapTime
	}
	return 0
}

// isLapPitted checks if a lap included a pit stop by examining the lap events.
//
// Parameters:
//   - lap: The lap to check
//
// Returns:
//   - bool: true if the lap included a pit stop, false otherwise
func isLapPitted(lap Lap) bool {
	for _, event := range lap.LapEvents {
		if event == "pitted" {
			return true
		}
	}
	return false
}

// isLapValid determines if a lap should be counted as valid for average time calculations.
// A lap is invalid if it has any incidents, events, or invalid timing data.
//
// Parameters:
//   - lap: The lap to validate
//
// Returns:
//   - bool: true if the lap is valid, false otherwise
func isLapValid(lap Lap) bool {
	if lap.LapNumber <= 0 {
		return false
	}
	if lap.LapTime <= 0 {
		return false
	}
	if lap.Incident {
		return false
	}
	if len(lap.LapEvents) > 0 {
		return false
	}
	return true
}
