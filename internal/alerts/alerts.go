// Package alerts manages alert definitions, conditions, history, and evaluation.
package alerts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Operator constants for alert conditions.
const (
	OpGT            = ">"
	OpLT            = "<"
	OpGTE           = ">="
	OpLTE           = "<="
	OpChangesPctGT  = "changes_pct_gt"  // YoY% > threshold
	OpChangesPctLT  = "changes_pct_lt"  // YoY% < threshold
)

// Condition is one clause in a compound alert rule.
type Condition struct {
	ID        string  `json:"id"`
	AlertID   string  `json:"alert_id"`
	MetricKey string  `json:"metric_key"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Position  int     `json:"position"`
}

// Alert is a saved compound alert rule.
type Alert struct {
	ID                string      `json:"id"`
	UserID            string      `json:"user_id"`
	Name              string      `json:"name"`
	Enabled           bool        `json:"enabled"`
	CooldownHours     int         `json:"cooldown_hours"`
	ConsecutiveChecks int         `json:"consecutive_checks"`
	LastFiredAt       *string     `json:"last_fired_at,omitempty"`
	Conditions        []Condition `json:"conditions"`
	CreatedAt         string      `json:"created_at"`
}

// HistoryEntry is a single alert firing record.
type HistoryEntry struct {
	ID                string `json:"id"`
	AlertID           string `json:"alert_id"`
	FiredAt           string `json:"fired_at"`
	ConditionSnapshot string `json:"condition_snapshot"`
}

func newID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

// List returns all alerts for a user with their conditions.
func List(db *sql.DB, userID string) ([]Alert, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, enabled, cooldown_hours, consecutive_checks, last_fired_at, created_at
		 FROM alerts WHERE user_id=? ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Alert
	for rows.Next() {
		var a Alert
		var enabled int
		var lastFired sql.NullString
		if err := rows.Scan(&a.ID, &a.UserID, &a.Name, &enabled, &a.CooldownHours, &a.ConsecutiveChecks, &lastFired, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Enabled = enabled == 1
		if lastFired.Valid {
			s := lastFired.String
			a.LastFiredAt = &s
		}
		conds, err := listConditions(db, a.ID)
		if err != nil {
			return nil, err
		}
		a.Conditions = conds
		out = append(out, a)
	}
	return out, rows.Err()
}

func listConditions(db *sql.DB, alertID string) ([]Condition, error) {
	rows, err := db.Query(
		`SELECT id, alert_id, metric_key, operator, threshold, position
		 FROM alert_conditions WHERE alert_id=? ORDER BY position ASC`,
		alertID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Condition
	for rows.Next() {
		var c Condition
		if err := rows.Scan(&c.ID, &c.AlertID, &c.MetricKey, &c.Operator, &c.Threshold, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Create inserts a new alert with its conditions.
func Create(db *sql.DB, userID, name string, cooldownH, consecutiveChecks int, conditions []Condition) (*Alert, error) {
	id := newID()
	_, err := db.Exec(
		`INSERT INTO alerts (id, user_id, name, cooldown_hours, consecutive_checks) VALUES (?,?,?,?,?)`,
		id, userID, name, cooldownH, consecutiveChecks,
	)
	if err != nil {
		return nil, err
	}
	for i, c := range conditions {
		cid := fmt.Sprintf("%x-%d", time.Now().UnixNano(), i)
		if _, err := db.Exec(
			`INSERT INTO alert_conditions (id, alert_id, metric_key, operator, threshold, position) VALUES (?,?,?,?,?,?)`,
			cid, id, c.MetricKey, c.Operator, c.Threshold, i,
		); err != nil {
			return nil, err
		}
	}
	return get(db, id, userID)
}

func get(db *sql.DB, id, userID string) (*Alert, error) {
	var a Alert
	var enabled int
	var lastFired sql.NullString
	err := db.QueryRow(
		`SELECT id, user_id, name, enabled, cooldown_hours, consecutive_checks, last_fired_at, created_at
		 FROM alerts WHERE id=? AND user_id=?`,
		id, userID,
	).Scan(&a.ID, &a.UserID, &a.Name, &enabled, &a.CooldownHours, &a.ConsecutiveChecks, &lastFired, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Enabled = enabled == 1
	if lastFired.Valid {
		s := lastFired.String
		a.LastFiredAt = &s
	}
	conds, err := listConditions(db, a.ID)
	if err != nil {
		return nil, err
	}
	a.Conditions = conds
	return &a, nil
}

// Update replaces an alert's name, cooldown, enabled state, and conditions.
func Update(db *sql.DB, id, userID, name string, enabled bool, cooldownH, consecutiveChecks int, conditions []Condition) (*Alert, error) {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	if _, err := db.Exec(
		`UPDATE alerts SET name=?, enabled=?, cooldown_hours=?, consecutive_checks=? WHERE id=? AND user_id=?`,
		name, enabledInt, cooldownH, consecutiveChecks, id, userID,
	); err != nil {
		return nil, err
	}
	// Replace conditions.
	if _, err := db.Exec(`DELETE FROM alert_conditions WHERE alert_id=?`, id); err != nil {
		return nil, err
	}
	for i, c := range conditions {
		cid := fmt.Sprintf("%x-%d", time.Now().UnixNano(), i)
		if _, err := db.Exec(
			`INSERT INTO alert_conditions (id, alert_id, metric_key, operator, threshold, position) VALUES (?,?,?,?,?,?)`,
			cid, id, c.MetricKey, c.Operator, c.Threshold, i,
		); err != nil {
			return nil, err
		}
	}
	return get(db, id, userID)
}

// Delete removes an alert and its conditions (via ON DELETE CASCADE).
func Delete(db *sql.DB, id, userID string) error {
	_, err := db.Exec(`DELETE FROM alerts WHERE id=? AND user_id=?`, id, userID)
	return err
}

// CountForUser returns how many alerts a user has configured.
func CountForUser(db *sql.DB, userID string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM alerts WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

// FiresThisMonth returns how many times any of a user's alerts fired this calendar month.
func FiresThisMonth(db *sql.DB, userID string) (int, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM alert_history
		 WHERE user_id=? AND fired_at >= date('now','start of month')`,
		userID,
	).Scan(&n)
	return n, err
}

// GetHistory returns the last N firings for an alert.
func GetHistory(db *sql.DB, alertID, userID string, limit int) ([]HistoryEntry, error) {
	rows, err := db.Query(
		`SELECT h.id, h.alert_id, h.fired_at, h.condition_snapshot
		 FROM alert_history h
		 JOIN alerts a ON a.id=h.alert_id
		 WHERE h.alert_id=? AND a.user_id=?
		 ORDER BY h.fired_at DESC LIMIT ?`,
		alertID, userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		if err := rows.Scan(&e.ID, &e.AlertID, &e.FiredAt, &e.ConditionSnapshot); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EvaluateResult describes one alert that should fire.
type EvaluateResult struct {
	Alert      Alert
	UserEmail  string
	Conditions []string // human-readable condition summaries
}

// Evaluate checks all enabled alerts for all users against current metric values.
// Returns the set of alerts that should fire (caller sends emails and records history).
func Evaluate(db *sql.DB, metrics map[string]float64) ([]EvaluateResult, error) {
	// Fetch all enabled alerts with their owners.
	rows, err := db.Query(
		`SELECT a.id, a.user_id, a.name, a.cooldown_hours, a.last_fired_at, u.email
		 FROM alerts a JOIN users u ON u.id=a.user_id
		 WHERE a.enabled=1`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type alertRow struct {
		id, userID, name, email string
		cooldownH               int
		lastFired               sql.NullString
	}
	var alertRows []alertRow
	for rows.Next() {
		var r alertRow
		if err := rows.Scan(&r.id, &r.userID, &r.name, &r.cooldownH, &r.lastFired, &r.email); err != nil {
			return nil, err
		}
		alertRows = append(alertRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []EvaluateResult
	for _, ar := range alertRows {
		// Check cooldown.
		if ar.lastFired.Valid {
			t, err := time.Parse("2006-01-02T15:04:05Z", ar.lastFired.String)
			if err == nil && time.Since(t) < time.Duration(ar.cooldownH)*time.Hour {
				continue // still in cooldown
			}
		}

		conds, err := listConditions(db, ar.id)
		if err != nil {
			continue
		}
		if len(conds) == 0 {
			continue
		}

		allMet := true
		var summaries []string
		for _, c := range conds {
			val, ok := metrics[c.MetricKey]
			if !ok {
				allMet = false
				break
			}
			met := evaluateCondition(val, c.Operator, c.Threshold)
			if !met {
				allMet = false
				break
			}
			summaries = append(summaries, formatCondition(c.MetricKey, c.Operator, c.Threshold, val))
		}
		if !allMet {
			continue
		}

		a, err := get(db, ar.id, ar.userID)
		if err != nil || a == nil {
			continue
		}
		results = append(results, EvaluateResult{Alert: *a, UserEmail: ar.email, Conditions: summaries})
	}
	return results, nil
}

func evaluateCondition(val float64, op string, threshold float64) bool {
	switch op {
	case OpGT:
		return val > threshold
	case OpLT:
		return val < threshold
	case OpGTE:
		return val >= threshold
	case OpLTE:
		return val <= threshold
	default:
		return false
	}
}

// MetricLabels maps metric keys to human-readable names.
var MetricLabels = map[string]string{
	"system_stress":    "System Stress Score",
	"recession_risk":   "Recession Risk",
	"financial_stress": "Financial Stress",
	"consumer_health":  "Consumer Health",
	"housing_stress":   "Housing Stress",
	"macro_markets":    "Macro & Markets",
	"yield_credit":     "Yield & Credit",
	"overdose_deaths":  "Drug Overdose Deaths",
	"anxiety_pct":      "Anxiety Prevalence %",
}

func formatCondition(key, op string, threshold, val float64) string {
	label := MetricLabels[key]
	if label == "" {
		label = key
	}
	opLabel := map[string]string{
		OpGT: ">", OpLT: "<", OpGTE: "≥", OpLTE: "≤",
	}[op]
	return fmt.Sprintf("%s %s %.2f (current: %.2f)", label, opLabel, threshold, val)
}

// RecordFiring writes a history entry and updates last_fired_at.
func RecordFiring(db *sql.DB, alertID, userID string, conditions []string) error {
	id := fmt.Sprintf("%x", time.Now().UnixNano())
	snap, _ := json.Marshal(conditions)
	_, err := db.Exec(
		`INSERT INTO alert_history (id, alert_id, user_id, condition_snapshot) VALUES (?,?,?,?)`,
		id, alertID, userID, string(snap),
	)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE alerts SET last_fired_at=datetime('now') WHERE id=?`, alertID)
	return err
}
