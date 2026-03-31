package indicators

import (
	"keep-it-mobile/internal/fred"
	"testing"
)

func TestComputeSahm(t *testing.T) {
	// Build 24 months of observations: stable at 4.0% then a spike to 4.8%
	obs := make([]fred.Observation, 24)
	for i := range obs {
		obs[i].Value = "4.0"
		obs[i].Date = "2024-01-01"
	}
	// Spike recent months (indices 0-2 = latest) to trigger Sahm
	obs[0].Value = "4.8"
	obs[1].Value = "4.7"
	obs[2].Value = "4.6"

	res, err := ComputeSahm(obs)
	if err != nil {
		t.Fatalf("ComputeSahm failed: %v", err)
	}
	// latestMA3 ≈ 4.7, minPriorMA3 = 4.0 → sahmValue ≈ 0.7 → Critical
	if res.Status != Critical && res.Status != Stressed {
		t.Errorf("Expected Critical or Stressed for triggered Sahm, got %s (value=%.2f)", res.Status, res.Value)
	}
}
