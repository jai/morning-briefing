package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Output structure for LLM consumption
type MorningBriefing struct {
	GeneratedAt    string         `json:"generated_at"`
	TargetDate     string         `json:"target_date"`
	Sleep          SleepData      `json:"sleep"`
	Vitals         VitalsData     `json:"vitals"`
	Calendar       CalendarData   `json:"calendar"`
	Meds           MedsData       `json:"meds"`
	Training       TrainingData   `json:"training"`
	Classification Classification `json:"classification"`
	Errors         []string       `json:"errors,omitempty"`
}

type TrainingData struct {
	LastWorkout     *WorkoutSummary `json:"last_workout,omitempty"`
	DaysSinceLast   int             `json:"days_since_last"`
	RecentWorkouts  []WorkoutSummary `json:"recent_workouts,omitempty"`
	WeeklyCount     int             `json:"weekly_count"`
}

type WorkoutSummary struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Date        string   `json:"date"`
	Duration    string   `json:"duration"`
	Exercises   []string `json:"exercises"`
}

type SleepData struct {
	TotalHours    *float64 `json:"total_hours,omitempty"`
	DeepHours     *float64 `json:"deep_hours,omitempty"`
	REMHours      *float64 `json:"rem_hours,omitempty"`
	CoreHours     *float64 `json:"core_hours,omitempty"`
	DataDate      string   `json:"data_date,omitempty"`
	IsCurrentDay  bool     `json:"is_current_day"`
	DataAvailable bool     `json:"data_available"`
}

type VitalsData struct {
	RestingHR       *float64 `json:"resting_hr_bpm,omitempty"`
	HRV             *float64 `json:"hrv_ms,omitempty"`
	SpO2            *float64 `json:"spo2_pct,omitempty"`
	RespiratoryRate *float64 `json:"respiratory_rate,omitempty"`
}

type CalendarData struct {
	MorningEvents   []CalendarEvent `json:"morning_events"`
	AfternoonEvents []CalendarEvent `json:"afternoon_events"`
	MorningCount    int             `json:"morning_count"`
	FirstEventTime  string          `json:"first_event_time,omitempty"`
}

type CalendarEvent struct {
	Time    string `json:"time"`
	Summary string `json:"summary"`
	Source  string `json:"source"` // personal or work
}

type MedsData struct {
	DueToday  []MedTask `json:"due_today"`
	Overdue   []MedTask `json:"overdue"`
	Completed []MedTask `json:"completed"`
}

type MedTask struct {
	Name    string `json:"name"`
	DueTime string `json:"due_time,omitempty"`
	DueDate string `json:"due_date"`
}

type Classification struct {
	SleepQuality   string `json:"sleep_quality"`    // GOOD, OK, POOR, UNKNOWN
	MorningLoad    string `json:"morning_load"`     // CLEAR, LIGHT, PACKED
	RecoveryStatus string `json:"recovery_status"`  // GOOD, OK, POOR, UNKNOWN (based on HRV)
	Recommendation string `json:"recommendation"`   // Brief advice
}

// Health ingest summary structure
type HealthSummary struct {
	LatestStats map[string]struct {
		Value     float64 `json:"Value"`
		Unit      string  `json:"Unit"`
		Timestamp string  `json:"Timestamp"`
	} `json:"LatestStats"`
}

// Todoist response structure
type TodoistResponse struct {
	Results []struct {
		Content     string   `json:"content"`
		Labels      []string `json:"labels"`
		IsCompleted bool     `json:"is_completed"`
		Due         *struct {
			Date     string `json:"date"`
			DateTime string `json:"datetime"`
		} `json:"due"`
	} `json:"results"`
}

// Calendar response from gog
type GogCalendarResponse struct {
	Events []GogCalendarEvent `json:"events"`
}

type GogCalendarEvent struct {
	Start struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"start"`
	Summary string `json:"summary"`
}

func main() {
	now := time.Now()
	today := now.Format("2006-01-02")
	
	briefing := MorningBriefing{
		GeneratedAt: now.Format(time.RFC3339),
		TargetDate:  today,
	}

	// 1. Get health data (from health-ingest CLI and SQLite)
	getHealthData(&briefing, today)
	getHealthDataFromSQLite(&briefing, today)

	// 2. Get calendar data (both personal and work)
	getCalendarData(&briefing, today)

	// 3. Get meds from Todoist
	getMedsData(&briefing, today)

	// 4. Get training data from Hevy
	getTrainingData(&briefing, today)

	// 5. Classify and recommend
	classify(&briefing)

	// Output JSON
	output, _ := json.MarshalIndent(briefing, "", "  ")
	fmt.Println(string(output))
}

func getHealthData(b *MorningBriefing, today string) {
	// Run health-ingest summary
	cmd := exec.Command("health-ingest", "summary", "--json")
	output, err := cmd.Output()
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("health-ingest error: %v", err))
		return
	}

	var summary HealthSummary
	if err := json.Unmarshal(output, &summary); err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("health JSON parse error: %v", err))
		return
	}

	// Sleep data with date validation
	if sleep, ok := summary.LatestStats["sleep_total"]; ok {
		b.Sleep.DataAvailable = true
		b.Sleep.TotalHours = &sleep.Value
		b.Sleep.DataDate = sleep.Timestamp
		
		// Parse timestamp and check if it's from today or yesterday (valid for last night's sleep)
		// Sleep data timestamped at midnight belongs to the previous night
		if strings.Contains(sleep.Timestamp, today) || strings.Contains(sleep.Timestamp, yesterday(today)) {
			b.Sleep.IsCurrentDay = true
		}
	}

	if deep, ok := summary.LatestStats["sleep_deep"]; ok {
		b.Sleep.DeepHours = &deep.Value
	}

	if rem, ok := summary.LatestStats["sleep_rem"]; ok {
		b.Sleep.REMHours = &rem.Value
	}

	// Vitals
	if rhr, ok := summary.LatestStats["resting_heart_rate"]; ok {
		b.Vitals.RestingHR = &rhr.Value
	}
	if hrv, ok := summary.LatestStats["heart_rate_variability"]; ok {
		b.Vitals.HRV = &hrv.Value
	}
	if spo2, ok := summary.LatestStats["blood_oxygen_saturation"]; ok {
		b.Vitals.SpO2 = &spo2.Value
	}
}

func getCalendarData(b *MorningBriefing, today string) {
	// Personal calendar
	getCalendarEvents(b, today, "jai@govindani.com", "personal")
	
	// Work calendar
	getCalendarEvents(b, today, "jai.g@ewa-services.com", "work")

	b.Calendar.MorningCount = len(b.Calendar.MorningEvents)
	
	if len(b.Calendar.MorningEvents) > 0 {
		b.Calendar.FirstEventTime = b.Calendar.MorningEvents[0].Time
	}
}

func getCalendarEvents(b *MorningBriefing, today, account, source string) {
	cmd := exec.Command("gog", "calendar", "events", "--account="+account, "--json")
	output, err := cmd.Output()
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("calendar error (%s): %v", source, err))
		return
	}

	var resp GogCalendarResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("calendar JSON parse error (%s): %v", source, err))
		return
	}

	for _, e := range resp.Events {
		startTime := e.Start.DateTime
		if startTime == "" {
			continue // Skip all-day events
		}
		
		if !strings.HasPrefix(startTime, today) {
			continue // Not today
		}

		// Parse time
		t, err := time.Parse(time.RFC3339, startTime)
		if err != nil {
			continue
		}

		hour := t.Hour()
		event := CalendarEvent{
			Time:    t.Format("15:04"),
			Summary: e.Summary,
			Source:  source,
		}

		if hour < 12 {
			b.Calendar.MorningEvents = append(b.Calendar.MorningEvents, event)
		} else if hour < 18 {
			b.Calendar.AfternoonEvents = append(b.Calendar.AfternoonEvents, event)
		}
	}
}

func getMedsData(b *MorningBriefing, today string) {
	cmd := exec.Command("td", "today", "--json")
	output, err := cmd.Output()
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("todoist error: %v", err))
		return
	}

	var resp TodoistResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("todoist JSON parse error: %v", err))
		return
	}

	for _, task := range resp.Results {
		// Check if it's a med task
		isMed := false
		for _, label := range task.Labels {
			if label == "💊Meds" || label == "💉" {
				isMed = true
				break
			}
		}
		if !isMed {
			continue
		}

		med := MedTask{Name: task.Content}
		if task.Due != nil {
			med.DueDate = task.Due.Date
			if task.Due.DateTime != "" {
				if t, err := time.Parse(time.RFC3339, task.Due.DateTime); err == nil {
					med.DueTime = t.Format("15:04")
				}
			}
		}

		if task.IsCompleted {
			b.Meds.Completed = append(b.Meds.Completed, med)
		} else if task.Due != nil && task.Due.Date < today {
			b.Meds.Overdue = append(b.Meds.Overdue, med)
		} else {
			b.Meds.DueToday = append(b.Meds.DueToday, med)
		}
	}
}

// Hevy workout response
type HevyWorkout struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	StartTime string `json:"startTime"`
	Duration  string `json:"duration"`
	Exercises []struct {
		Name string `json:"name"`
	} `json:"exercises"`
}

func getTrainingData(b *MorningBriefing, today string) {
	cmd := exec.Command("mcporter", "call", "hevy.get-workouts", "page=1", "pageSize=10")
	output, err := cmd.Output()
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("hevy error: %v", err))
		return
	}

	var workouts []HevyWorkout
	if err := json.Unmarshal(output, &workouts); err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("hevy JSON parse error: %v", err))
		return
	}

	if len(workouts) == 0 {
		return
	}

	// Calculate days since last workout
	now := time.Now()
	weekAgo := now.AddDate(0, 0, -7)
	weeklyCount := 0

	for i, w := range workouts {
		workoutDate, err := time.Parse(time.RFC3339, w.StartTime)
		if err != nil {
			continue
		}

		exercises := make([]string, 0, len(w.Exercises))
		for _, e := range w.Exercises {
			exercises = append(exercises, e.Name)
		}

		summary := WorkoutSummary{
			ID:        w.ID,
			Title:     w.Title,
			Date:      workoutDate.Format("2006-01-02"),
			Duration:  w.Duration,
			Exercises: exercises,
		}

		if i == 0 {
			b.Training.LastWorkout = &summary
			b.Training.DaysSinceLast = int(now.Sub(workoutDate).Hours() / 24)
		}

		if workoutDate.After(weekAgo) {
			weeklyCount++
		}

		b.Training.RecentWorkouts = append(b.Training.RecentWorkouts, summary)
	}

	b.Training.WeeklyCount = weeklyCount
}

func classify(b *MorningBriefing) {
	// Sleep quality (factoring in deep sleep)
	if !b.Sleep.DataAvailable || !b.Sleep.IsCurrentDay {
		b.Classification.SleepQuality = "UNKNOWN"
	} else if b.Sleep.TotalHours != nil {
		hours := *b.Sleep.TotalHours
		switch {
		case hours >= 7:
			b.Classification.SleepQuality = "GOOD"
		case hours >= 5:
			b.Classification.SleepQuality = "OK"
		default:
			b.Classification.SleepQuality = "POOR"
		}

		// Downgrade sleep quality if deep sleep is insufficient (<1hr)
		if b.Sleep.DeepHours != nil && *b.Sleep.DeepHours < 1.0 {
			switch b.Classification.SleepQuality {
			case "GOOD":
				b.Classification.SleepQuality = "OK"
			case "OK":
				b.Classification.SleepQuality = "POOR"
			}
		}
	}

	// Recovery status based on HRV
	if b.Vitals.HRV == nil {
		b.Classification.RecoveryStatus = "UNKNOWN"
	} else {
		hrv := *b.Vitals.HRV
		switch {
		case hrv <= 20:
			b.Classification.RecoveryStatus = "POOR"
		case hrv < 40:
			b.Classification.RecoveryStatus = "OK"
		default:
			b.Classification.RecoveryStatus = "GOOD"
		}
	}

	// Morning load
	count := b.Calendar.MorningCount
	switch {
	case count == 0:
		b.Classification.MorningLoad = "CLEAR"
	case count <= 2:
		b.Classification.MorningLoad = "LIGHT"
	default:
		b.Classification.MorningLoad = "PACKED"
	}

	// Generate recommendation (now includes recovery status)
	sleep := b.Classification.SleepQuality
	load := b.Classification.MorningLoad
	recovery := b.Classification.RecoveryStatus

	// Poor recovery takes priority in recommendations
	if recovery == "POOR" && b.Vitals.HRV != nil {
		if sleep == "POOR" {
			b.Classification.Recommendation = "Poor sleep + poor recovery (low HRV). Take it very easy today, prioritize rest and recovery."
		} else {
			b.Classification.Recommendation = fmt.Sprintf("HRV is low (%.0fms) indicating poor recovery. Consider lighter activity today.", *b.Vitals.HRV)
		}
		return
	}

	switch {
	case sleep == "POOR" && load == "PACKED":
		b.Classification.Recommendation = "Rough night + packed morning. Prioritize must-dos, defer what you can. Power through essentials only."
	case sleep == "POOR" && load == "LIGHT":
		b.Classification.Recommendation = "Rough night but light morning. Ease in, handle the few things, then reassess energy."
	case sleep == "POOR" && load == "CLEAR":
		b.Classification.Recommendation = "Rough night, clear morning. Take it slow, no rush. Recovery day vibes."
	case sleep == "OK" && load == "PACKED":
		b.Classification.Recommendation = "Decent sleep, busy morning. You've got this, stay focused."
	case sleep == "GOOD":
		b.Classification.Recommendation = "Well rested. Attack the day."
	default:
		b.Classification.Recommendation = "Sleep data unavailable. Check energy levels and adjust accordingly."
	}
}

func yesterday(today string) string {
	t, _ := time.Parse("2006-01-02", today)
	return t.AddDate(0, 0, -1).Format("2006-01-02")
}

// SQLite database path
func getHealthDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".health-ingest", "health.db")
}

// Query average HRV for a given date from SQLite
func queryAverageHRV(db *sql.DB, date string) (*float64, error) {
	query := `
		SELECT AVG(value) FROM metrics 
		WHERE metric_name = 'heart_rate_variability' 
		AND timestamp LIKE ? || '%'
	`
	var avg sql.NullFloat64
	err := db.QueryRow(query, date).Scan(&avg)
	if err != nil {
		return nil, err
	}
	if !avg.Valid {
		return nil, nil
	}
	return &avg.Float64, nil
}

// Query sleep stages for a given date from SQLite
func querySleepStages(db *sql.DB, date string) (deep, rem, core *float64, err error) {
	query := `
		SELECT metric_name, value FROM metrics 
		WHERE metric_name IN ('sleep_deep', 'sleep_rem', 'sleep_core')
		AND timestamp LIKE ? || '%'
	`
	rows, err := db.Query(query, date)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var value float64
		if err := rows.Scan(&name, &value); err != nil {
			continue
		}
		v := value
		switch name {
		case "sleep_deep":
			deep = &v
		case "sleep_rem":
			rem = &v
		case "sleep_core":
			core = &v
		}
	}
	return deep, rem, core, nil
}

// Query latest respiratory rate for a given date from SQLite
func queryLatestRespiratoryRate(db *sql.DB, date string) (*float64, error) {
	query := `
		SELECT value FROM metrics 
		WHERE metric_name = 'respiratory_rate' 
		AND timestamp LIKE ? || '%'
		ORDER BY timestamp DESC 
		LIMIT 1
	`
	var value sql.NullFloat64
	err := db.QueryRow(query, date).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if !value.Valid {
		return nil, nil
	}
	return &value.Float64, nil
}

// Fetch additional metrics from SQLite database
func getHealthDataFromSQLite(b *MorningBriefing, today string) {
	dbPath := getHealthDBPath()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("sqlite open error: %v", err))
		return
	}
	defer db.Close()

	// Get average HRV for today
	avgHRV, err := queryAverageHRV(db, today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("HRV query error: %v", err))
	} else if avgHRV != nil {
		b.Vitals.HRV = avgHRV
	}

	// Get sleep stages
	deep, rem, core, err := querySleepStages(db, today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("sleep stages query error: %v", err))
	} else {
		if deep != nil {
			b.Sleep.DeepHours = deep
		}
		if rem != nil {
			b.Sleep.REMHours = rem
		}
		if core != nil {
			b.Sleep.CoreHours = core
		}
	}

	// Get latest respiratory rate
	rr, err := queryLatestRespiratoryRate(db, today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("respiratory rate query error: %v", err))
	} else if rr != nil {
		b.Vitals.RespiratoryRate = rr
	}
}
