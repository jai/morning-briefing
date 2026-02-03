package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// User stats for calculations
const (
	UserAge             = 41
	UserWeightKg        = 73.0
	UserHeightCm        = 177.0
	UserIsMale          = true
	UserBMRKcal         = 1636 // Mifflin-St Jeor result
	UserProteinTargetG  = 152
)

// EveningBriefing is the output structure for evening wrap-up
type EveningBriefing struct {
	Mode        string        `json:"mode"`
	GeneratedAt string        `json:"generated_at"`
	TargetDate  string        `json:"target_date"`
	Energy      EnergyData    `json:"energy"`
	Protein     ProteinData   `json:"protein"`
	Activity    ActivityData  `json:"activity"`
	Recovery    RecoveryData  `json:"recovery"`
	Protocols   ProtocolsData `json:"protocols"`
	Tomorrow    TomorrowData  `json:"tomorrow"`
	Errors      []string      `json:"errors,omitempty"`
}

type EnergyData struct {
	DeficitOrSurplusKcal int     `json:"deficit_or_surplus_kcal"`
	Status               string  `json:"status"` // "deficit", "surplus", "maintenance"
	BMRKcal              int     `json:"bmr_kcal"`
	ActiveKcal           float64 `json:"active_kcal"`
	TotalBurnedKcal      float64 `json:"total_burned_kcal"`
	ConsumedKcal         float64 `json:"consumed_kcal"`
}

type ProteinData struct {
	ConsumedG  float64 `json:"consumed_g"`
	TargetG    int     `json:"target_g"`
	RemainingG float64 `json:"remaining_g"`
	OnTrack    bool    `json:"on_track"`
}

type ActivityData struct {
	Steps      int          `json:"steps"`
	Workout    *WorkoutInfo `json:"workout,omitempty"`
	StandHours int          `json:"stand_hours"`
}

type WorkoutInfo struct {
	Done     bool   `json:"done"`
	Title    string `json:"title,omitempty"`
	Duration string `json:"duration,omitempty"`
}

type RecoveryData struct {
	HRVMS          float64   `json:"hrv_ms"`
	HRVYesterdayMS float64   `json:"hrv_yesterday_ms"`
	RestingHRBPM   float64   `json:"resting_hr_bpm"`
	SleepLastNight SleepInfo `json:"sleep_last_night"`
}

type SleepInfo struct {
	TotalHrs float64 `json:"total_hrs"`
	DeepHrs  float64 `json:"deep_hrs"`
}

type ProtocolsData struct {
	Completed []string `json:"completed"`
	Missed    []string `json:"missed"`
}

type TomorrowData struct {
	FirstEvent       *EventInfo `json:"first_event,omitempty"`
	WorkoutScheduled bool       `json:"workout_scheduled"`
	MedsDue          []string   `json:"meds_due"`
}

type EventInfo struct {
	Time    string `json:"time"`
	Summary string `json:"summary"`
}

// CalculateBMR calculates Basal Metabolic Rate using Mifflin-St Jeor formula
// Men: BMR = (10 × weight in kg) + (6.25 × height in cm) - (5 × age) + 5
// Women: BMR = (10 × weight in kg) + (6.25 × height in cm) - (5 × age) - 161
func CalculateBMR(weightKg, heightCm float64, age int, isMale bool) int {
	bmr := (10 * weightKg) + (6.25 * heightCm) - (5 * float64(age))
	if isMale {
		bmr += 5
	} else {
		bmr -= 161
	}
	return int(bmr + 0.5) // Round to nearest int
}

// CalculateEnergyBalance calculates caloric deficit or surplus
// Returns: balance (negative = deficit), status string
func CalculateEnergyBalance(bmr int, activeEnergy, consumedEnergy float64) (int, string) {
	totalBurned := float64(bmr) + activeEnergy
	balance := int(consumedEnergy - totalBurned + 0.5)

	var status string
	if balance < -50 {
		status = "deficit"
	} else if balance > 50 {
		status = "surplus"
	} else {
		status = "maintenance"
	}

	return balance, status
}

// CalculateProteinStatus calculates remaining protein needed
// Returns: remaining grams, whether on track (>=95% of target)
func CalculateProteinStatus(consumed, target float64) (float64, bool) {
	remaining := target - consumed
	if remaining < 0 {
		remaining = 0
	}

	// On track if consumed >= 95% of target
	onTrack := consumed >= (target * 0.95)

	return remaining, onTrack
}

// ParseMode determines the briefing mode from CLI flags
func ParseMode(morning, evening bool) (string, error) {
	if morning && evening {
		return "", errors.New("cannot specify both --morning and --evening")
	}
	if evening {
		return "evening", nil
	}
	return "morning", nil
}

// RunEveningBriefing generates the evening wrap-up output
func RunEveningBriefing() {
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterdayDate := yesterday(today)

	briefing := EveningBriefing{
		Mode:        "evening",
		GeneratedAt: now.Format(time.RFC3339),
		TargetDate:  today,
		Energy: EnergyData{
			BMRKcal: UserBMRKcal,
		},
		Protein: ProteinData{
			TargetG: UserProteinTargetG,
		},
		Protocols: ProtocolsData{
			Completed: []string{},
			Missed:    []string{},
		},
		Tomorrow: TomorrowData{
			MedsDue: []string{},
		},
	}

	// Get data from health-ingest SQLite
	getEveningHealthData(&briefing, today, yesterdayDate)

	// Get today's workout from Hevy
	getEveningWorkoutData(&briefing, today)

	// Get protocol completion from Todoist
	getEveningProtocolData(&briefing, today)

	// Get tomorrow's preview
	getTomorrowData(&briefing, today)

	// Output JSON
	output, _ := json.MarshalIndent(briefing, "", "  ")
	fmt.Println(string(output))
}

func getEveningHealthData(b *EveningBriefing, today, yesterday string) {
	dbPath := getHealthDBPath()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("sqlite open error: %v", err))
		return
	}
	defer db.Close()

	// Get active energy for today
	activeEnergy, err := queryDayTotal(db, "active_energy", today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("active_energy query error: %v", err))
	} else {
		b.Energy.ActiveKcal = activeEnergy
	}

	// Get dietary energy (consumed) for today
	consumedEnergy, err := queryDayTotal(db, "dietary_energy", today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("dietary_energy query error: %v", err))
	} else {
		b.Energy.ConsumedKcal = consumedEnergy
	}

	// Calculate energy balance
	b.Energy.TotalBurnedKcal = float64(b.Energy.BMRKcal) + b.Energy.ActiveKcal
	b.Energy.DeficitOrSurplusKcal, b.Energy.Status = CalculateEnergyBalance(
		b.Energy.BMRKcal, b.Energy.ActiveKcal, b.Energy.ConsumedKcal)

	// Get protein for today
	protein, err := queryDayTotal(db, "protein", today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("protein query error: %v", err))
	} else {
		b.Protein.ConsumedG = protein
		b.Protein.RemainingG, b.Protein.OnTrack = CalculateProteinStatus(protein, float64(b.Protein.TargetG))
	}

	// Get steps for today
	steps, err := queryDayTotal(db, "steps", today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("steps query error: %v", err))
	} else {
		b.Activity.Steps = int(steps)
	}

	// Get stand hours for today
	standHours, err := queryDayTotal(db, "stand_hours", today)
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("stand_hours query error: %v", err))
	} else {
		b.Activity.StandHours = int(standHours)
	}

	// Get HRV for today
	hrvToday, err := queryAverageHRV(db, today)
	if err == nil && hrvToday != nil {
		b.Recovery.HRVMS = *hrvToday
	}

	// Get HRV for yesterday
	hrvYesterday, err := queryAverageHRV(db, yesterday)
	if err == nil && hrvYesterday != nil {
		b.Recovery.HRVYesterdayMS = *hrvYesterday
	}

	// Get resting HR
	rhr, err := queryLatestValue(db, "resting_heart_rate", today)
	if err == nil && rhr != nil {
		b.Recovery.RestingHRBPM = *rhr
	}

	// Get last night's sleep (use today's date - sleep recorded for end date)
	sleepTotal, err := queryLatestValue(db, "sleep_total", today)
	if err == nil && sleepTotal != nil {
		b.Recovery.SleepLastNight.TotalHrs = *sleepTotal
	}

	sleepDeep, err := queryLatestValue(db, "sleep_deep", today)
	if err == nil && sleepDeep != nil {
		b.Recovery.SleepLastNight.DeepHrs = *sleepDeep
	}
}

func queryDayTotal(db *sql.DB, metricName, date string) (float64, error) {
	query := `
		SELECT COALESCE(SUM(value), 0) FROM metrics 
		WHERE metric_name = ? 
		AND timestamp LIKE ? || '%'
	`
	var total float64
	err := db.QueryRow(query, metricName, date).Scan(&total)
	return total, err
}

func queryLatestValue(db *sql.DB, metricName, date string) (*float64, error) {
	query := `
		SELECT value FROM metrics 
		WHERE metric_name = ? 
		AND timestamp LIKE ? || '%'
		ORDER BY timestamp DESC 
		LIMIT 1
	`
	var value sql.NullFloat64
	err := db.QueryRow(query, metricName, date).Scan(&value)
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

func getEveningWorkoutData(b *EveningBriefing, today string) {
	cmd := exec.Command("mcporter", "call", "hevy.get-workouts", "page=1", "pageSize=5")
	output, err := cmd.Output()
	if err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("hevy error: %v", err))
		b.Activity.Workout = &WorkoutInfo{Done: false}
		return
	}

	var workouts []HevyWorkout
	if err := json.Unmarshal(output, &workouts); err != nil {
		b.Errors = append(b.Errors, fmt.Sprintf("hevy JSON parse error: %v", err))
		b.Activity.Workout = &WorkoutInfo{Done: false}
		return
	}

	// Check if any workout is from today
	b.Activity.Workout = &WorkoutInfo{Done: false}
	for _, w := range workouts {
		if strings.HasPrefix(w.StartTime, today) {
			b.Activity.Workout = &WorkoutInfo{
				Done:     true,
				Title:    w.Title,
				Duration: w.Duration,
			}
			break
		}
	}
}

func getEveningProtocolData(b *EveningBriefing, today string) {
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
		// Check if it's a med/protocol task
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

		if task.IsCompleted {
			b.Protocols.Completed = append(b.Protocols.Completed, task.Content)
		} else {
			// Check if overdue or just not done yet today
			if task.Due != nil && task.Due.Date <= today {
				b.Protocols.Missed = append(b.Protocols.Missed, task.Content)
			}
		}
	}
}

func getTomorrowData(b *EveningBriefing, today string) {
	tomorrow := addDays(today, 1)

	// Get tomorrow's calendar events
	getTomorrowCalendar(b, tomorrow)

	// Get tomorrow's meds from Todoist
	getTomorrowMeds(b, tomorrow)
}

func getTomorrowCalendar(b *EveningBriefing, tomorrow string) {
	// Personal calendar
	events := getCalendarEventsForDate(b, tomorrow, "jai@govindani.com")
	events = append(events, getCalendarEventsForDate(b, tomorrow, "jai.g@ewa-services.com")...)

	if len(events) == 0 {
		return
	}

	// Find first event
	var firstEvent *EventInfo
	var firstTime time.Time

	for _, e := range events {
		if firstEvent == nil || e.parsedTime.Before(firstTime) {
			firstTime = e.parsedTime
			firstEvent = &EventInfo{
				Time:    e.Time,
				Summary: e.Summary,
			}
		}

		// Check if it's a workout
		lowerSummary := strings.ToLower(e.Summary)
		if strings.Contains(lowerSummary, "workout") || strings.Contains(lowerSummary, "gym") ||
			strings.Contains(lowerSummary, "training") || strings.Contains(lowerSummary, "jesper") {
			b.Tomorrow.WorkoutScheduled = true
		}
	}

	b.Tomorrow.FirstEvent = firstEvent
}

type calendarEventWithTime struct {
	CalendarEvent
	parsedTime time.Time
}

func getCalendarEventsForDate(b *EveningBriefing, date, account string) []calendarEventWithTime {
	cmd := exec.Command("gog", "calendar", "events", "--account="+account, "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var resp GogCalendarResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil
	}

	var events []calendarEventWithTime
	for _, e := range resp.Events {
		startTime := e.Start.DateTime
		if startTime == "" {
			continue // Skip all-day events
		}

		if !strings.HasPrefix(startTime, date) {
			continue
		}

		t, err := time.Parse(time.RFC3339, startTime)
		if err != nil {
			continue
		}

		events = append(events, calendarEventWithTime{
			CalendarEvent: CalendarEvent{
				Time:    t.Format("15:04"),
				Summary: e.Summary,
			},
			parsedTime: t,
		})
	}

	return events
}

func getTomorrowMeds(b *EveningBriefing, tomorrow string) {
	// Query Todoist for tomorrow's meds
	cmd := exec.Command("td", "filter", fmt.Sprintf("due: %s", tomorrow), "--json")
	output, err := cmd.Output()
	if err != nil {
		// Try alternative: list upcoming
		return
	}

	var resp TodoistResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return
	}

	for _, task := range resp.Results {
		isMed := false
		for _, label := range task.Labels {
			if label == "💊Meds" || label == "💉" {
				isMed = true
				break
			}
		}
		if isMed {
			b.Tomorrow.MedsDue = append(b.Tomorrow.MedsDue, task.Content)
		}
	}
}

func addDays(date string, days int) string {
	t, _ := time.Parse("2006-01-02", date)
	return t.AddDate(0, 0, days).Format("2006-01-02")
}
