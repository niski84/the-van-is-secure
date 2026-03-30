package indicators

import (
	"testing"
)

func TestScoringFunctions(t *testing.T) {
	t.Run("YieldCurve", func(t *testing.T) {
		res, err := ScoreYieldCurve("Test", "T10Y2Y", "2024-01-01", 0.6)
		if err != nil {
			t.Errorf("ScoreYieldCurve returned error: %v", err)
		}
		if res.Status != Green {
			t.Errorf("Expected Green, got %s", res.Status)
		}

		res, err = ScoreYieldCurve("Test", "T10Y2Y", "2024-01-01", 0.2)
		if res.Status != Yellow {
			t.Errorf("Expected Yellow, got %s", res.Status)
		}

		res, err = ScoreYieldCurve("Test", "T10Y2Y", "2024-01-01", -0.1)
		if res.Status != Red {
			t.Errorf("Expected Red, got %s", res.Status)
		}
	})

	t.Run("CreditSpread", func(t *testing.T) {
		res, err := ScoreCreditSpread("Test", "BAA10YM", "2024-01-01", 1.5)
		if err != nil {
			t.Errorf("ScoreCreditSpread returned error: %v", err)
		}
		if res.Status != Green {
			t.Errorf("Expected Green, got %s", res.Status)
		}

		res, err = ScoreCreditSpread("Test", "BAA10YM", "2024-01-01", 2.5)
		if res.Status != Yellow {
			t.Errorf("Expected Yellow, got %s", res.Status)
		}

		res, err = ScoreCreditSpread("Test", "BAA10YM", "2024-01-01", 3.5)
		if res.Status != Red {
			t.Errorf("Expected Red, got %s", res.Status)
		}
	})
}

