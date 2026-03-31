package indicators

import (
	"fmt"
	"keep-it-mobile/internal/fred"
	"log"
)

func ComputeSahm(observations []fred.Observation) (*IndicatorResult, error) {
	log.Printf("Computing Sahm Rule from %d observations", len(observations))

	if len(observations) < 15 { // Need at least 3 for latest MA, and some for prior 12 months
		return nil, fmt.Errorf("insufficient data for Sahm Rule: need at least 15 observations, got %d", len(observations))
	}

	// Observations are desc (latest first)
	values := make([]float64, 0, len(observations))
	dates := make([]string, 0, len(observations))
	for _, obs := range observations {
		v, err := fred.ParseFloat(obs.Value)
		if err != nil {
			continue // Skip missing data
		}
		values = append(values, v)
		dates = append(dates, obs.Date)
	}

	if len(values) < 15 {
		return nil, fmt.Errorf("insufficient valid data for Sahm Rule after filtering")
	}

	// latest MA3 = average of values[0], values[1], values[2]
	latestMA3 := (values[0] + values[1] + values[2]) / 3.0

	// minPrior12MA3 = min of MA3s over the prior 12 months (excluding latest month)
	// The prior 12 months starts from values[1] to values[12]? 
	// Actually, the Sahm rule usually compares the current 3-month average 
	// to the lowest 3-month average in the previous 12 months.
	
	var minPriorMA3 float64
	first := true

	// We look at the 12 months preceding the current month.
	// Current month is index 0. Prior 12 months are indices 1 to 12.
	// For each of those months i, we need the 3-month average: (values[i] + values[i+1] + values[i+2]) / 3
	// So we need up to index 12+2 = 14.
	for i := 1; i <= 12; i++ {
		if i+2 >= len(values) {
			break
		}
		ma3 := (values[i] + values[i+1] + values[i+2]) / 3.0
		if first || ma3 < minPriorMA3 {
			minPriorMA3 = ma3
			first = true
		}
	}

	sahmValue := latestMA3 - minPriorMA3
	log.Printf("Sahm calculation: latestMA3=%f, minPriorMA3=%f, sahm=%f", latestMA3, minPriorMA3, sahmValue)

	var status Status
	switch {
	case sahmValue >= 0.70:
		status = Critical
	case sahmValue >= 0.50:
		status = Stressed // triggered
	case sahmValue >= 0.35:
		status = Elevated // approaching trigger
	case sahmValue >= 0.20:
		status = Watch
	default:
		status = Clear
	}

	return &IndicatorResult{
		Name:   "Sahm Rule",
		Series: "UNRATE",
		Date:   dates[0],
		Status: status,
		Value:  sahmValue,
	}, nil
}

