package indicators

import (
	"keep-it-mobile/internal/fred"
	"testing"
)

func TestComputeSahm(t *testing.T) {
	// Synthetic UNRATE data
	// Sahm = latestMA3 - minPrior12MA3
	// latestMA3 = (6.0 + 6.0 + 6.0) / 3 = 6.0
	// prior MA3s: (4.0 + 4.0 + 4.0) / 3 = 4.0
	// Sahm = 6.0 - 4.0 = 2.0 (RED)
	
	obs := []fred.Observation{
		{Date: "2024-01-01", Value: "6.0"},
		{Date: "2023-12-01", Value: "6.0"},
		{Date: "2023-11-01", Value: "6.0"},
		{Date: "2023-10-01", Value: "4.0"},
		{Date: "2023-09-01", Value: "4.0"},
		{Date: "2023-08-01", Value: "4.0"},
		{Date: "2023-07-01", Value: "4.0"},
		{Date: "2023-06-01", Value: "4.0"},
		{Date: "2023-05-01", Value: "4.0"},
		{Date: "2023-04-01", Value: "4.0"},
		{Date: "2023-03-01", Value: "4.0"},
		{Date: "2023-02-01", Value: "4.0"},
		{Date: "2023-01-01", Value: "4.0"},
		{Date: "2022-12-01", Value: "4.0"},
		{Date: "2022-11-01", Value: "4.0"},
	}

	res, err := ComputeSahm(obs)
	if err != nil {
		t.Fatalf("ComputeSahm failed: %v", err)
	}

	if res.Status != Red {
		t.Errorf("Expected RED status, got %s", res.Status)
	}
}

