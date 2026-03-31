package indicators

import (
	"fmt"
	"keep-it-mobile/internal/fred"
	"log"
	"math"
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

// ScoreJoblessClaims scores initial jobless claims (ICSA). Unit: raw count.
func ScoreJoblessClaims(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring jobless claims %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 350000 {
		status = Red
	} else if value >= 280000 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.0fK/wk", value/1000),
	}, nil
}

// ScoreSahmDirect scores the Sahm Rule real-time indicator directly (SAHMREALTIME).
func ScoreSahmDirect(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring Sahm direct %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 0.50 {
		status = Red
	} else if value >= 0.35 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.2f", value),
	}, nil
}

// ScoreVIX scores the CBOE Volatility Index (VIXCLS).
func ScoreVIX(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring VIX %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 30 {
		status = Red
	} else if value >= 20 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.1f", value),
	}, nil
}

// ScoreFinancialStress scores a financial stress index (STLFSI4 or NFCI).
// Values above 0 indicate above-average stress; yellowThresh and redThresh are the thresholds.
func ScoreFinancialStress(name, series, date string, value, yellowThresh, redThresh float64) (*IndicatorResult, error) {
	log.Printf("Scoring financial stress %s (%s): value=%f, date=%s", name, series, value, date)
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
		Note:   fmt.Sprintf("%.3f", value),
	}, nil
}

// ScoreSavingsRate scores the personal saving rate (PSAVERT). Higher is better.
func ScoreSavingsRate(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring savings rate %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value < 3.0 {
		status = Red
	} else if value < 6.0 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.1f%%", value),
	}, nil
}

// ScoreU6 scores the U-6 broad unemployment rate (U6RATE).
func ScoreU6(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring U-6 %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 12.0 {
		status = Red
	} else if value >= 9.0 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.1f%%", value),
	}, nil
}

// ScoreConsumerSentiment scores the U of Michigan consumer sentiment index (UMCSENT). Higher is better.
func ScoreConsumerSentiment(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring consumer sentiment %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value < 60.0 {
		status = Red
	} else if value < 75.0 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.1f", value),
	}, nil
}

// ScoreDebtGDP scores the federal debt as a percent of GDP (GFDEGDQ188S).
func ScoreDebtGDP(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring debt/GDP %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 130.0 {
		status = Red
	} else if value >= 100.0 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.1f%%", value),
	}, nil
}

// ScoreOilPrice scores the WTI crude oil spot price (DCOILWTICO).
func ScoreOilPrice(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring oil price %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 100.0 {
		status = Red
	} else if value >= 80.0 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("$%.2f/bbl", value),
	}, nil
}

// ScoreBreakevenInflation scores the 10-year breakeven inflation rate (T10YIE).
func ScoreBreakevenInflation(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring breakeven inflation %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 3.0 {
		status = Red
	} else if value >= 2.5 {
		status = Yellow
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   date,
		Status: status,
		Value:  value,
		Note:   fmt.Sprintf("%.2f%%", value),
	}, nil
}

// ScoreMortgageDelinquency scores the single-family residential mortgage delinquency rate (DRSFRMACBS).
func ScoreMortgageDelinquency(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring mortgage delinquency %s (%s): value=%f, date=%s", name, series, value, date)
	status := Green
	if value >= 4.0 {
		status = Red
	} else if value >= 2.5 {
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

// ScoreSeriouslyDelinquent scores 90+ day past due / foreclosure rates (RCMFLB* series).
// These are balance-weighted rates at large banks and include loans in active foreclosure.
func ScoreSeriouslyDelinquent(name, series, date string, value, yellowThresh, redThresh float64) (*IndicatorResult, error) {
	log.Printf("Scoring seriously delinquent %s (%s): value=%f, date=%s", name, series, value, date)
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
		Note:   fmt.Sprintf("%.2f%%", value),
	}, nil
}

// ScoreYoYChange computes a year-over-year (or period-over-period) percent change and scores it.
// observations must be in descending order (latest first).
// nLookback is how many observations back to compare (e.g. 12 for monthly YoY, 252 for daily YoY).
// If higherIsBad=true, a positive change is stress (e.g. debt); if false, a decline is stress (e.g. stocks).
func ScoreYoYChange(name, series string, observations []fred.Observation, nLookback int, yellowThresh, redThresh float64, higherIsBad bool) (*IndicatorResult, error) {
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
	needed := nLookback + 1
	if len(vals) < needed {
		return nil, fmt.Errorf("ScoreYoYChange %s: need %d observations, got %d", series, needed, len(vals))
	}
	if vals[nLookback] == 0 {
		return nil, fmt.Errorf("ScoreYoYChange %s: prior value is zero", series)
	}

	yoy := (vals[0] - vals[nLookback]) / math.Abs(vals[nLookback]) * 100
	log.Printf("YoY %s: current=%f, prior=%f, yoy=%.2f%%", series, vals[0], vals[nLookback], yoy)

	// Determine stress: if higherIsBad, rising = bad; if !higherIsBad, falling = bad
	stress := yoy
	if !higherIsBad {
		stress = -yoy
	}

	status := Green
	if stress >= redThresh {
		status = Red
	} else if stress >= yellowThresh {
		status = Yellow
	}

	arrow := "+"
	if yoy < 0 {
		arrow = ""
	}
	return &IndicatorResult{
		Name:   name,
		Series: series,
		Date:   dates[0],
		Status: status,
		Value:  yoy,
		Note:   fmt.Sprintf("%s%.1f%% YoY", arrow, yoy),
	}, nil
}

