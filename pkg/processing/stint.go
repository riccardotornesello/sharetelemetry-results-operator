package processing

func GetAverageTimeMs(laps []Lap, requiredValidLaps int) int64 {
	var validLaps int = 0
	var totalLapTime int64 = 0

	for _, lap := range laps {
		if lap.LapNumber == -1 || isLapPitted(lap) {
			if validLaps > 0 {
				return 0
			}
			continue
		}

		if !isLapValid(lap) {
			return 0
		}

		validLaps += 1
		totalLapTime += lap.LapTime

		if validLaps >= requiredValidLaps {
			break
		}
	}

	if validLaps == requiredValidLaps {
		totalLapTime /= (10 * int64(requiredValidLaps))
		return totalLapTime
	}
	return 0
}

func isLapPitted(lap Lap) bool {
	for _, event := range lap.LapEvents {
		if event == "pitted" {
			return true
		}
	}
	return false
}

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
