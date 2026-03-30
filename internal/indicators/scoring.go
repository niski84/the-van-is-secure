package indicators

import (
	"fmt"
	"keep-it-mobile/internal/fred"
	"log"
)

type Status string

const (
	Green  Status = "GREEN"
	Yellow Status = "YELLOW"
	Red    Status = "RED"
)

type IndicatorResult struct {
	Name   string  `json:"name"`
	Series string  `json:"series"`
	Date   string  `json:"date"`
	Status Status  `json:"status"`
	Value  float64 `json:"value"`
	Note   string  `json:"note"`
}

func ScoreYieldCurve(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring yield curve %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value < 0 {
		status = Red
	} else if value <= 0.50 {
		status = Yellow
	}

	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
	}, nil
}

func ScoreCreditSpread(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring credit spread %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 3.00 {
		status = Red
	} else if value >= 2.00 {
		status = Yellow
	}

	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
	}, nil
}

// ScoreGasPrice scores US regular gasoline price ($/gallon).
func ScoreGasPrice(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring gas price %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value > 4.25 {
		status = Red
	} else if value >= 3.50 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("$%.2f/gal", value),
	}, nil
}

// ScoreMortgageRate scores the 30-year fixed mortgage rate (%).
func ScoreMortgageRate(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring mortgage rate %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value > 7.0 {
		status = Red
	} else if value >= 5.5 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.2f%% APR", value),
	}, nil
}

// ScoreRentalVacancy scores the rental vacancy rate (%). Higher vacancy = more supply = lower pressure.
func ScoreRentalVacancy(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring rental vacancy %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value < 5.0 {
		status = Red
	} else if value < 7.0 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.1f%% vacant", value),
	}, nil
}

// ScoreRentYoY computes year-over-year rent inflation from the CPI rent component.
// Observations must be in descending order (latest first); requires at least 13 months.
func ScoreRentYoY(observations []fred.Observation) (*IndicatorResult, error) {
	var vals []float64
	var dates []string
	for _, obs := range observations {
		v, err := fred.ParseFloat(obs.Value)
		if err != nil {
			continue
		}
		vals = append(vals, v)
		dates = append(dates, obs.Date)
	}
	if len(vals) < 13 {
		return nil, fmt.Errorf("ScoreRentYoY: need 13 observations, got %d", len(vals))
	}
	if vals[12] == 0 {
		return nil, fmt.Errorf("ScoreRentYoY: prior year value is zero")
	}

	yoy := (vals[0] - vals[12]) / vals[12] * 100
	log.Printf("Rent YoY: current=%f, priorYear=%f, yoy=%.2f%%", vals[0], vals[12], yoy)

	status := Green
	if yoy >= 7.0 {
		status = Red
	} else if yoy >= 4.0 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   "Rent Inflation YoY",
		Series: "CUSR0000SEHA",
		Date:   dates[0],
		Status: status,
		Value:  yoy,
		Note:   fmt.Sprintf("+%.1f%% YoY", yoy),
	}, nil
}

// ScoreConsumerDelinquency scores a generic consumer loan delinquency rate (%).
func ScoreConsumerDelinquency(name, series, date string, value float64, redThresh, yellowThresh float64) (*IndicatorResult, error) {
	log.Printf("Scoring consumer delinquency %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= redThresh {
		status = Red
	} else if value >= yellowThresh {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.2f%% past due", value),
	}, nil
}

// ScoreChargeOffRate scores a loan charge-off rate (%).
func ScoreChargeOffRate(name, series, date string, value float64, redThresh, yellowThresh float64) (*IndicatorResult, error) {
	log.Printf("Scoring charge-off rate %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= redThresh {
		status = Red
	} else if value >= yellowThresh {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.2f%% charged off", value),
	}, nil
}

func ScoreRecessionProb(name, series, date string, value float64, err error) (*IndicatorResult, error) {
	log.Printf("Scoring recession prob %s (%s): value=%f, date=%s, err=%v", name, series, value, date, err)
	if err != nil {
		return &IndicatorResult{
			Name:   name,
			Series: series,
			Date:   "N/A",
			Status: Yellow,
			Value:  0,
			Note:   fmt.Sprintf("unavailable in this run: %v", err),
		}, nil
	}

	status := Green
	if value >= 0.30 {
		status = Red
	} else if value >= 0.20 {
		status = Yellow
	}

	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
	}, nil
}

