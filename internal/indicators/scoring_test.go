package indicators

import (
	"testing"
)

func TestScoringFunctions(t *testing.T) {
	t.Run("YieldCurve", func(t *testing.T) {
		res, err := ScoreYieldCurve("Test", "T10Y2Y", "2024-01-01", 1.0)
		if err != nil {
			t.Errorf("ScoreYieldCurve returned error: %v", err)
		}
		if res.Status != Clear {
			t.Errorf("Expected Clear, got %s", res.Status)
		}
		res, _ = ScoreYieldCurve("Test", "T10Y2Y", "2024-01-01", 0.1)
		if res.Status != Elevated {
			t.Errorf("Expected Elevated, got %s", res.Status)
		}
		res, _ = ScoreYieldCurve("Test", "T10Y2Y", "2024-01-01", -0.1)
		if res.Status != Stressed {
			t.Errorf("Expected Stressed, got %s", res.Status)
		}
		res, _ = ScoreYieldCurve("Test", "T10Y2Y", "2024-01-01", -0.8)
		if res.Status != Critical {
			t.Errorf("Expected Critical, got %s", res.Status)
		}
	})

	t.Run("CreditSpread", func(t *testing.T) {
		res, err := ScoreCreditSpread("Test", "BAA10YM", "2024-01-01", 1.2)
		if err != nil {
			t.Errorf("ScoreCreditSpread returned error: %v", err)
		}
		if res.Status != Clear {
			t.Errorf("Expected Clear, got %s", res.Status)
		}
		res, _ = ScoreCreditSpread("Test", "BAA10YM", "2024-01-01", 2.3)
		if res.Status != Elevated {
			t.Errorf("Expected Elevated, got %s", res.Status)
		}
		res, _ = ScoreCreditSpread("Test", "BAA10YM", "2024-01-01", 4.0)
		if res.Status != Critical {
			t.Errorf("Expected Critical, got %s", res.Status)
		}
	})
}
