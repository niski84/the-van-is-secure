// Package entitlements provides static solvency data for US entitlement programs.
// Source: SSA Trustees Report 2024, CMS Medicare Trustees Report 2024, PBGC FY2023 Annual Report.
// Update this file annually when the Trustees Reports are released (typically May).
package entitlements

import "time"

type Fund struct {
	Key              string  `json:"key"`
	Name             string  `json:"name"`
	Icon             string  `json:"icon"`
	DepletionYear    int     `json:"depletion_year"`    // 9999 = no depletion projected
	YearsRemaining   int     `json:"years_remaining"`   // computed at request time
	FundedPct        float64 `json:"funded_pct"`        // trust fund ratio as % of 1yr expenditure
	BenefitCutPct    float64 `json:"benefit_cut_pct"`   // % reduction if depleted, no action taken
	BalanceBillions  float64 `json:"balance_billions"`  // current trust fund balance
	Status           string  `json:"status"`            // CLEAR/WATCH/ELEVATED/STRESSED/CRITICAL
	PopCoveredM      float64 `json:"pop_covered_m"`     // millions covered
	Note             string  `json:"note"`
}

type Response struct {
	Funds      []Fund `json:"funds"`
	AsOf       string `json:"as_of"`        // report year
	UpdatedBy  string `json:"updated_by"`   // source report
	FetchedAt  string `json:"fetched_at"`
}

// yearsStatus assigns a stress level based on proximity to depletion.
func yearsStatus(years int) string {
	switch {
	case years < 5:
		return "CRITICAL"
	case years < 10:
		return "STRESSED"
	case years < 15:
		return "ELEVATED"
	case years < 20:
		return "WATCH"
	default:
		return "CLEAR"
	}
}

// Get returns the current entitlements snapshot with years_remaining computed from now.
func Get() Response {
	currentYear := time.Now().Year()

	funds := []Fund{
		{
			Key:             "oasi",
			Name:            "Social Security (OASI)",
			Icon:            "👴",
			DepletionYear:   2033,
			FundedPct:       227,
			BenefitCutPct:   21,
			BalanceBillions: 2_669,
			PopCoveredM:     67,
			Note:            "Old-Age & Survivors Insurance trust fund. At depletion, ongoing payroll taxes cover ~79% of scheduled benefits with no legislative action. Combined OASDI (including Disability) depletes 2035.",
		},
		{
			Key:             "di",
			Name:            "Social Security Disability (DI)",
			Icon:            "♿",
			DepletionYear:   2098,
			FundedPct:       460,
			BenefitCutPct:   0,
			BalanceBillions: 168,
			PopCoveredM:     8.4,
			Note:            "Disability Insurance trust fund is in strong shape — projected solvent through 2098 under intermediate assumptions. Recent reforms reduced strain significantly.",
		},
		{
			Key:             "medicare_hi",
			Name:            "Medicare Part A (HI)",
			Icon:            "🏥",
			DepletionYear:   2036,
			FundedPct:       100,
			BenefitCutPct:   11,
			BalanceBillions: 289,
			PopCoveredM:     66,
			Note:            "Hospital Insurance trust fund. Parts B (outpatient) and D (drugs) are funded through premiums and general revenue with no depletion risk. Part A is the constrained piece.",
		},
		{
			Key:             "pbgc_single",
			Name:            "PBGC — Private Pensions",
			Icon:            "🏢",
			DepletionYear:   9999,
			FundedPct:       0, // surplus, not ratio
			BenefitCutPct:   0,
			BalanceBillions: 41.7,
			PopCoveredM:     30,
			Note:            "Pension Benefit Guaranty Corporation single-employer fund — $41.7B surplus as of FY2023, the strongest position in PBGC history. Backstops ~22,700 private pension plans if sponsors fail.",
		},
		{
			Key:             "pbgc_multi",
			Name:            "PBGC — Multiemployer",
			Icon:            "👷",
			DepletionYear:   9999,
			FundedPct:       0,
			BenefitCutPct:   0,
			BalanceBillions: 1.5,
			PopCoveredM:     10.9,
			Note:            "Multiemployer fund covers union/trade pensions. Was projected insolvent by 2026; rescued by the American Rescue Plan (2021) which injected ~$91B. Now projected solvent through at least 2050.",
		},
	}

	for i, f := range funds {
		if f.DepletionYear == 9999 {
			funds[i].YearsRemaining = 9999
			funds[i].Status = "CLEAR"
		} else {
			yr := f.DepletionYear - currentYear
			if yr < 0 {
				yr = 0
			}
			funds[i].YearsRemaining = yr
			funds[i].Status = yearsStatus(yr)
		}
	}

	return Response{
		Funds:     funds,
		AsOf:      "2024",
		UpdatedBy: "SSA Trustees Report 2024 · CMS Medicare Trustees Report 2024 · PBGC FY2023 Annual Report",
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
