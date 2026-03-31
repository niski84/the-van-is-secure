package indicators

import (
	"fmt"
	"keep-it-mobile/internal/fred"
	"log"
	"math"
)

type Status string

const (
	Clear    Status = "CLEAR"    // nominal — all good
	Watch    Status = "WATCH"    // minor deviation — worth noting
	Elevated Status = "ELEVATED" // stress building — pay attention
	Stressed Status = "STRESSED" // significant — preparation warranted
	Critical Status = "CRITICAL" // crisis conditions — act now
)

type IndicatorResult struct {
	Name   string  `json:"name"`
	Series string  `json:"series"`
	Date   string  `json:"date"`
	Status Status  `json:"status"`
	Value  float64 `json:"value"`
	Note   string  `json:"note"`
}

// level5 classifies value against four ascending thresholds (watch, elevated, stressed, critical).
// If higherIsBad=true, higher values move up the scale; if false, lower values are worse.
func level5(value, watchT, elevatedT, stressedT, criticalT float64, higherIsBad bool) Status {
	stress := value
	if !higherIsBad {
		stress = -value
		watchT, elevatedT, stressedT, criticalT = -watchT, -elevatedT, -stressedT, -criticalT
		// flip sign so we can compare uniformly
		watchT, elevatedT, stressedT, criticalT = -criticalT, -stressedT, -elevatedT, -watchT
		stress = -value
	}
	switch {
	case stress >= criticalT:
		return Critical
	case stress >= stressedT:
		return Stressed
	case stress >= elevatedT:
		return Elevated
	case stress >= watchT:
		return Watch
	default:
		return Clear
	}
}

func ScoreYieldCurve(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring yield curve %s (%s): value=%f, date=%s", name, series, value, date)
	// Positive = normal (long > short). Inversion historically precedes recessions.
	var status Status
	switch {
	case value < -0.50:
		status = Critical // deeply inverted
	case value < 0:
		status = Stressed // inverted
	case value < 0.25:
		status = Elevated // approaching inversion
	case value < 0.75:
		status = Watch // narrow spread
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value}, nil
}

func ScoreCreditSpread(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring credit spread %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 3.75:
		status = Critical
	case value >= 2.75:
		status = Stressed
	case value >= 2.00:
		status = Elevated
	case value >= 1.50:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value}, nil
}

func ScoreGasPrice(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring gas price %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 5.00:
		status = Critical
	case value >= 4.00:
		status = Stressed
	case value >= 3.25:
		status = Elevated
	case value >= 2.75:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("$%.2f/gal", value)}, nil
}

func ScoreMortgageRate(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring mortgage rate %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 8.50:
		status = Critical
	case value >= 7.00:
		status = Stressed
	case value >= 6.00:
		status = Elevated
	case value >= 5.00:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.2f%% APR", value)}, nil
}

// ScoreRentalVacancy scores the rental vacancy rate (%). Higher vacancy = more supply = lower stress.
func ScoreRentalVacancy(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring rental vacancy %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value < 4.5:
		status = Critical
	case value < 5.5:
		status = Stressed
	case value < 6.5:
		status = Elevated
	case value < 7.5:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.1f%% vacant", value)}, nil
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
	var status Status
	switch {
	case yoy >= 9.0:
		status = Critical
	case yoy >= 6.0:
		status = Stressed
	case yoy >= 4.0:
		status = Elevated
	case yoy >= 2.5:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{
		Name: "Rent Inflation YoY", Series: "CUSR0000SEHA", Date: dates[0],
		Status: status, Value: yoy, Note: fmt.Sprintf("+%.1f%% YoY", yoy),
	}, nil
}

// ScoreConsumerDelinquency scores a consumer loan delinquency rate (%).
// Thresholds in ascending stress order: watch, elevated, stressed, critical.
func ScoreConsumerDelinquency(name, series, date string, value, watchT, elevatedT, stressedT, criticalT float64) (*IndicatorResult, error) {
	log.Printf("Scoring consumer delinquency %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= criticalT:
		status = Critical
	case value >= stressedT:
		status = Stressed
	case value >= elevatedT:
		status = Elevated
	case value >= watchT:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.2f%% past due", value)}, nil
}

// ScoreChargeOffRate scores a loan charge-off rate (%).
// Thresholds in ascending stress order: watch, elevated, stressed, critical.
func ScoreChargeOffRate(name, series, date string, value, watchT, elevatedT, stressedT, criticalT float64) (*IndicatorResult, error) {
	log.Printf("Scoring charge-off rate %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= criticalT:
		status = Critical
	case value >= stressedT:
		status = Stressed
	case value >= elevatedT:
		status = Elevated
	case value >= watchT:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.2f%% charged off", value)}, nil
}

func ScoreRecessionProb(name, series, date string, value float64, err error) (*IndicatorResult, error) {
	log.Printf("Scoring recession prob %s (%s): value=%f, date=%s, err=%v", name, series, value, date, err)
	if err != nil {
		return &IndicatorResult{
			Name: name, Series: series, Date: "N/A", Status: Watch, Value: 0,
			Note: fmt.Sprintf("unavailable in this run: %v", err),
		}, nil
	}
	var status Status
	switch {
	case value >= 0.60:
		status = Critical
	case value >= 0.35:
		status = Stressed
	case value >= 0.20:
		status = Elevated
	case value >= 0.10:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value}, nil
}

func ScoreJoblessClaims(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring jobless claims %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 400000:
		status = Critical
	case value >= 325000:
		status = Stressed
	case value >= 270000:
		status = Elevated
	case value >= 230000:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.0fK/wk", value/1000)}, nil
}

func ScoreSahmDirect(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring Sahm direct %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 0.70:
		status = Critical // well into recession
	case value >= 0.50:
		status = Stressed // triggered
	case value >= 0.35:
		status = Elevated // approaching trigger
	case value >= 0.20:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.2f", value)}, nil
}

func ScoreVIX(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring VIX %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 40:
		status = Critical
	case value >= 28:
		status = Stressed
	case value >= 20:
		status = Elevated
	case value >= 15:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.1f", value)}, nil
}

// ScoreFinancialStress scores a financial stress index (STLFSI4, NFCI).
// Thresholds in ascending stress order: watch, elevated, stressed, critical.
func ScoreFinancialStress(name, series, date string, value, watchT, elevatedT, stressedT, criticalT float64) (*IndicatorResult, error) {
	log.Printf("Scoring financial stress %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= criticalT:
		status = Critical
	case value >= stressedT:
		status = Stressed
	case value >= elevatedT:
		status = Elevated
	case value >= watchT:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.3f", value)}, nil
}

// ScoreSavingsRate scores the personal saving rate (PSAVERT). Higher is better (inverted scale).
func ScoreSavingsRate(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring savings rate %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value < 2.0:
		status = Critical
	case value < 3.5:
		status = Stressed
	case value < 5.0:
		status = Elevated
	case value < 8.0:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.1f%%", value)}, nil
}

func ScoreU6(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring U-6 %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 12.5:
		status = Critical
	case value >= 10.0:
		status = Stressed
	case value >= 8.0:
		status = Elevated
	case value >= 6.5:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.1f%%", value)}, nil
}

// ScoreConsumerSentiment scores the U of Michigan consumer sentiment index (UMCSENT). Higher is better.
func ScoreConsumerSentiment(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring consumer sentiment %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value < 48:
		status = Critical
	case value < 60:
		status = Stressed
	case value < 72:
		status = Elevated
	case value < 85:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.1f", value)}, nil
}

func ScoreDebtGDP(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring debt/GDP %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 135:
		status = Critical
	case value >= 115:
		status = Stressed
	case value >= 100:
		status = Elevated
	case value >= 80:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.1f%%", value)}, nil
}

func ScoreOilPrice(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring oil price %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 115:
		status = Critical
	case value >= 95:
		status = Stressed
	case value >= 80:
		status = Elevated
	case value >= 65:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("$%.2f/bbl", value)}, nil
}

func ScoreBreakevenInflation(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring breakeven inflation %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 3.75:
		status = Critical
	case value >= 3.00:
		status = Stressed
	case value >= 2.50:
		status = Elevated
	case value >= 2.00:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.2f%%", value)}, nil
}

func ScoreMortgageDelinquency(name, series, date string, value float64) (*IndicatorResult, error) {
	log.Printf("Scoring mortgage delinquency %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= 5.0:
		status = Critical
	case value >= 3.5:
		status = Stressed
	case value >= 2.5:
		status = Elevated
	case value >= 1.5:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.2f%% past due", value)}, nil
}

// ScoreSeriouslyDelinquent scores 90+ DPD / foreclosure rates at large banks.
// Thresholds in ascending stress order: watch, elevated, stressed, critical.
func ScoreSeriouslyDelinquent(name, series, date string, value, watchT, elevatedT, stressedT, criticalT float64) (*IndicatorResult, error) {
	log.Printf("Scoring seriously delinquent %s (%s): value=%f, date=%s", name, series, value, date)
	var status Status
	switch {
	case value >= criticalT:
		status = Critical
	case value >= stressedT:
		status = Stressed
	case value >= elevatedT:
		status = Elevated
	case value >= watchT:
		status = Watch
	default:
		status = Clear
	}
	return &IndicatorResult{Name: name, Series: series, Date: date, Status: status, Value: value,
		Note: fmt.Sprintf("%.2f%%", value)}, nil
}

// ScoreYoYChange computes a year-over-year percent change and scores it.
// Observations must be in descending order (latest first).
// Thresholds apply to the stress direction: if higherIsBad=true, rising % is stress;
// if false (e.g. stocks), falling % is stress. Thresholds are in ascending stress order.
func ScoreYoYChange(name, series string, observations []fred.Observation, nLookback int, watchT, elevatedT, stressedT, criticalT float64, higherIsBad bool) (*IndicatorResult, error) {
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

	stress := yoy
	if !higherIsBad {
		stress = -yoy
	}
	var status Status
	switch {
	case stress >= criticalT:
		status = Critical
	case stress >= stressedT:
		status = Stressed
	case stress >= elevatedT:
		status = Elevated
	case stress >= watchT:
		status = Watch
	default:
		status = Clear
	}
	arrow := "+"
	if yoy < 0 {
		arrow = ""
	}
	return &IndicatorResult{
		Name: name, Series: series, Date: dates[0],
		Status: status, Value: yoy, Note: fmt.Sprintf("%s%.1f%% YoY", arrow, yoy),
	}, nil
}
