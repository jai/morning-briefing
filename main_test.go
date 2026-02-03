package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Test the yesterday() helper function
func TestYesterday(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2024-01-15", "2024-01-14"},
		{"2024-01-01", "2023-12-31"}, // Year boundary
		{"2024-03-01", "2024-02-29"}, // Leap year
		{"2023-03-01", "2023-02-28"}, // Non-leap year
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := yesterday(tt.input)
			if result != tt.expected {
				t.Errorf("yesterday(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Test classify() sleep quality classification
func TestClassifySleepQuality(t *testing.T) {
	tests := []struct {
		name          string
		totalHours    *float64
		dataAvailable bool
		isCurrentDay  bool
		expected      string
	}{
		{"no data", nil, false, false, "UNKNOWN"},
		{"stale data", ptr(7.0), true, false, "UNKNOWN"},
		{"good sleep", ptr(7.5), true, true, "GOOD"},
		{"exactly 7 hours", ptr(7.0), true, true, "GOOD"},
		{"ok sleep", ptr(6.0), true, true, "OK"},
		{"exactly 5 hours", ptr(5.0), true, true, "OK"},
		{"poor sleep", ptr(4.5), true, true, "POOR"},
		{"very poor sleep", ptr(2.0), true, true, "POOR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &MorningBriefing{
				Sleep: SleepData{
					TotalHours:    tt.totalHours,
					DataAvailable: tt.dataAvailable,
					IsCurrentDay:  tt.isCurrentDay,
				},
			}
			classify(b)
			if b.Classification.SleepQuality != tt.expected {
				t.Errorf("classify() SleepQuality = %q, want %q", b.Classification.SleepQuality, tt.expected)
			}
		})
	}
}

// Test classify() morning load classification
func TestClassifyMorningLoad(t *testing.T) {
	tests := []struct {
		name     string
		events   int
		expected string
	}{
		{"no events", 0, "CLEAR"},
		{"one event", 1, "LIGHT"},
		{"two events", 2, "LIGHT"},
		{"three events", 3, "PACKED"},
		{"many events", 10, "PACKED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make([]CalendarEvent, tt.events)
			for i := range events {
				events[i] = CalendarEvent{Time: "09:00", Summary: "Test event"}
			}

			b := &MorningBriefing{
				Calendar: CalendarData{
					MorningEvents: events,
					MorningCount:  tt.events,
				},
				Sleep: SleepData{DataAvailable: false}, // Set unknown sleep to avoid nil pointer
			}
			classify(b)
			if b.Classification.MorningLoad != tt.expected {
				t.Errorf("classify() MorningLoad = %q, want %q", b.Classification.MorningLoad, tt.expected)
			}
		})
	}
}

// Test classify() recommendations
func TestClassifyRecommendations(t *testing.T) {
	tests := []struct {
		name         string
		sleepHours   *float64
		morningCount int
		sleepCurrent bool
		wantContains string
	}{
		{"poor sleep packed morning", ptr(3.0), 5, true, "Rough night + packed"},
		{"poor sleep light morning", ptr(3.0), 1, true, "Rough night but light"},
		{"poor sleep clear morning", ptr(3.0), 0, true, "Rough night, clear morning"},
		{"ok sleep packed morning", ptr(6.0), 4, true, "Decent sleep, busy morning"},
		{"good sleep", ptr(8.0), 2, true, "Well rested"},
		{"unknown sleep", nil, 2, false, "Sleep data unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make([]CalendarEvent, tt.morningCount)
			for i := range events {
				events[i] = CalendarEvent{Time: "09:00", Summary: "Test"}
			}

			b := &MorningBriefing{
				Sleep: SleepData{
					TotalHours:    tt.sleepHours,
					DataAvailable: tt.sleepHours != nil,
					IsCurrentDay:  tt.sleepCurrent,
				},
				Calendar: CalendarData{
					MorningEvents: events,
					MorningCount:  tt.morningCount,
				},
			}
			classify(b)
			if !contains(b.Classification.Recommendation, tt.wantContains) {
				t.Errorf("classify() Recommendation = %q, want to contain %q", b.Classification.Recommendation, tt.wantContains)
			}
		})
	}
}

// Test JSON parsing for health-ingest response
func TestHealthSummaryParsing(t *testing.T) {
	jsonData := `{
		"LatestStats": {
			"sleep_total": {"Value": 7.5, "Unit": "hours", "Timestamp": "2024-01-15T00:00:00Z"},
			"sleep_deep": {"Value": 1.2, "Unit": "hours", "Timestamp": "2024-01-15T00:00:00Z"},
			"sleep_rem": {"Value": 1.8, "Unit": "hours", "Timestamp": "2024-01-15T00:00:00Z"},
			"resting_heart_rate": {"Value": 52, "Unit": "bpm", "Timestamp": "2024-01-15T00:00:00Z"},
			"heart_rate_variability": {"Value": 45, "Unit": "ms", "Timestamp": "2024-01-15T00:00:00Z"},
			"blood_oxygen_saturation": {"Value": 98, "Unit": "%", "Timestamp": "2024-01-15T00:00:00Z"}
		}
	}`

	var summary HealthSummary
	err := json.Unmarshal([]byte(jsonData), &summary)
	if err != nil {
		t.Fatalf("Failed to parse HealthSummary: %v", err)
	}

	if sleep, ok := summary.LatestStats["sleep_total"]; !ok || sleep.Value != 7.5 {
		t.Errorf("sleep_total = %v, want 7.5", summary.LatestStats["sleep_total"])
	}

	if rhr, ok := summary.LatestStats["resting_heart_rate"]; !ok || rhr.Value != 52 {
		t.Errorf("resting_heart_rate = %v, want 52", summary.LatestStats["resting_heart_rate"])
	}
}

// Test JSON parsing for Todoist response
func TestTodoistResponseParsing(t *testing.T) {
	jsonData := `{
		"results": [
			{
				"content": "Take vitamin D",
				"labels": ["💊Meds"],
				"is_completed": false,
				"due": {"date": "2024-01-15", "datetime": "2024-01-15T08:00:00+07:00"}
			},
			{
				"content": "HCG injection",
				"labels": ["💉"],
				"is_completed": true,
				"due": {"date": "2024-01-15", "datetime": "2024-01-15T07:00:00+07:00"}
			},
			{
				"content": "Buy groceries",
				"labels": ["errands"],
				"is_completed": false,
				"due": {"date": "2024-01-15"}
			}
		]
	}`

	var resp TodoistResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	if err != nil {
		t.Fatalf("Failed to parse TodoistResponse: %v", err)
	}

	if len(resp.Results) != 3 {
		t.Errorf("len(Results) = %d, want 3", len(resp.Results))
	}

	// Check first task has med label
	found := false
	for _, label := range resp.Results[0].Labels {
		if label == "💊Meds" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("First task should have 💊Meds label")
	}

	// Check second task is completed
	if !resp.Results[1].IsCompleted {
		t.Errorf("Second task should be completed")
	}
}

// Test JSON parsing for calendar response
func TestGogCalendarResponseParsing(t *testing.T) {
	jsonData := `{
		"events": [
			{
				"start": {"dateTime": "2024-01-15T09:00:00+07:00"},
				"summary": "Team standup"
			},
			{
				"start": {"dateTime": "2024-01-15T14:00:00+07:00"},
				"summary": "Client call"
			},
			{
				"start": {"date": "2024-01-15"},
				"summary": "All day event"
			}
		]
	}`

	var resp GogCalendarResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	if err != nil {
		t.Fatalf("Failed to parse GogCalendarResponse: %v", err)
	}

	if len(resp.Events) != 3 {
		t.Errorf("len(Events) = %d, want 3", len(resp.Events))
	}

	if resp.Events[0].Summary != "Team standup" {
		t.Errorf("Events[0].Summary = %q, want %q", resp.Events[0].Summary, "Team standup")
	}

	// Check that dateTime is parsed correctly
	if resp.Events[0].Start.DateTime != "2024-01-15T09:00:00+07:00" {
		t.Errorf("Events[0].Start.DateTime = %q, want %q", resp.Events[0].Start.DateTime, "2024-01-15T09:00:00+07:00")
	}

	// Check all-day event has date but no dateTime
	if resp.Events[2].Start.Date != "2024-01-15" {
		t.Errorf("Events[2].Start.Date = %q, want %q", resp.Events[2].Start.Date, "2024-01-15")
	}
}

// Test JSON parsing for Hevy workout response
func TestHevyWorkoutParsing(t *testing.T) {
	jsonData := `[
		{
			"id": "workout-123",
			"title": "Full Body A",
			"startTime": "2024-01-14T10:00:00+07:00",
			"duration": "1h15m",
			"exercises": [
				{"name": "Squat"},
				{"name": "Bench Press"},
				{"name": "Deadlift"}
			]
		},
		{
			"id": "workout-122",
			"title": "Arms",
			"startTime": "2024-01-13T10:00:00+07:00",
			"duration": "45m",
			"exercises": [
				{"name": "Bicep Curl"},
				{"name": "Tricep Extension"}
			]
		}
	]`

	var workouts []HevyWorkout
	err := json.Unmarshal([]byte(jsonData), &workouts)
	if err != nil {
		t.Fatalf("Failed to parse HevyWorkout: %v", err)
	}

	if len(workouts) != 2 {
		t.Errorf("len(workouts) = %d, want 2", len(workouts))
	}

	if workouts[0].Title != "Full Body A" {
		t.Errorf("workouts[0].Title = %q, want %q", workouts[0].Title, "Full Body A")
	}

	if len(workouts[0].Exercises) != 3 {
		t.Errorf("len(workouts[0].Exercises) = %d, want 3", len(workouts[0].Exercises))
	}

	// Verify exercise names
	expectedExercises := []string{"Squat", "Bench Press", "Deadlift"}
	for i, e := range workouts[0].Exercises {
		if e.Name != expectedExercises[i] {
			t.Errorf("workouts[0].Exercises[%d].Name = %q, want %q", i, e.Name, expectedExercises[i])
		}
	}
}

// Test MorningBriefing JSON output structure
func TestMorningBriefingJSONOutput(t *testing.T) {
	now := time.Now()
	b := MorningBriefing{
		GeneratedAt: now.Format(time.RFC3339),
		TargetDate:  now.Format("2006-01-02"),
		Sleep: SleepData{
			TotalHours:    ptr(7.5),
			DataAvailable: true,
			IsCurrentDay:  true,
		},
		Classification: Classification{
			SleepQuality:   "GOOD",
			MorningLoad:    "LIGHT",
			Recommendation: "Well rested. Attack the day.",
		},
	}

	output, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal MorningBriefing: %v", err)
	}

	// Unmarshal back to verify round-trip
	var parsed MorningBriefing
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal MorningBriefing: %v", err)
	}

	if parsed.Classification.SleepQuality != "GOOD" {
		t.Errorf("parsed.Classification.SleepQuality = %q, want %q", parsed.Classification.SleepQuality, "GOOD")
	}

	if parsed.Sleep.TotalHours == nil || *parsed.Sleep.TotalHours != 7.5 {
		t.Errorf("parsed.Sleep.TotalHours = %v, want 7.5", parsed.Sleep.TotalHours)
	}
}

// Helper functions

func ptr(f float64) *float64 {
	return &f
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ==================== NEW TESTS FOR SQLITE METRICS ====================

// Test VitalsData includes new fields (HRV, RespiratoryRate)
func TestVitalsDataStructure(t *testing.T) {
	vitals := VitalsData{
		RestingHR:       ptr(52.0),
		HRV:             ptr(45.0),
		SpO2:            ptr(98.0),
		RespiratoryRate: ptr(12.5),
	}

	if vitals.RespiratoryRate == nil || *vitals.RespiratoryRate != 12.5 {
		t.Errorf("RespiratoryRate = %v, want 12.5", vitals.RespiratoryRate)
	}
}

// Test SleepData includes CoreHours
func TestSleepDataStructure(t *testing.T) {
	sleep := SleepData{
		TotalHours:    ptr(7.5),
		DeepHours:     ptr(1.2),
		REMHours:      ptr(1.5),
		CoreHours:     ptr(4.8),
		DataAvailable: true,
		IsCurrentDay:  true,
	}

	if sleep.CoreHours == nil || *sleep.CoreHours != 4.8 {
		t.Errorf("CoreHours = %v, want 4.8", sleep.CoreHours)
	}
}

// Test Classification includes RecoveryStatus
func TestClassificationStructure(t *testing.T) {
	c := Classification{
		SleepQuality:   "GOOD",
		MorningLoad:    "LIGHT",
		RecoveryStatus: "GOOD",
		Recommendation: "Well rested",
	}

	if c.RecoveryStatus != "GOOD" {
		t.Errorf("RecoveryStatus = %q, want %q", c.RecoveryStatus, "GOOD")
	}
}

// Test SQLite metric querying with test database
func TestQuerySQLiteMetrics(t *testing.T) {
	// Create temp database
	tmpDir, err := os.MkdirTemp("", "health-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "health.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create schema
	_, err = db.Exec(`
		CREATE TABLE metrics (
			id INTEGER PRIMARY KEY,
			file_date DATE,
			metric_name TEXT,
			timestamp TEXT,
			value REAL,
			unit TEXT,
			source TEXT,
			raw_json TEXT,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(metric_name, timestamp)
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Insert test data - today's date
	today := time.Now().Format("2006-01-02")
	todayTimestamp := today + " 00:00:00 +0700"
	_, err = db.Exec(`
		INSERT INTO metrics (metric_name, timestamp, value, unit) VALUES
		('heart_rate_variability', ?, 45.5, 'ms'),
		('heart_rate_variability', ?, 50.2, 'ms'),
		('sleep_deep', ?, 1.2, 'hr'),
		('sleep_rem', ?, 1.5, 'hr'),
		('sleep_core', ?, 4.8, 'hr'),
		('respiratory_rate', ?, 12.0, 'count/min')
	`, today+" 06:00:00 +0700", today+" 05:00:00 +0700",
		todayTimestamp, todayTimestamp, todayTimestamp, today+" 05:30:00 +0700")
	if err != nil {
		t.Fatal(err)
	}

	// Test querying average HRV
	avgHRV, err := queryAverageHRV(db, today)
	if err != nil {
		t.Errorf("queryAverageHRV error: %v", err)
	}
	if avgHRV == nil {
		t.Error("queryAverageHRV returned nil, expected value")
	} else if *avgHRV < 47 || *avgHRV > 48 {
		t.Errorf("queryAverageHRV = %v, want ~47.85", *avgHRV)
	}

	// Test querying sleep stages
	deep, rem, core, err := querySleepStages(db, today)
	if err != nil {
		t.Errorf("querySleepStages error: %v", err)
	}
	if deep == nil || *deep != 1.2 {
		t.Errorf("deep = %v, want 1.2", deep)
	}
	if rem == nil || *rem != 1.5 {
		t.Errorf("rem = %v, want 1.5", rem)
	}
	if core == nil || *core != 4.8 {
		t.Errorf("core = %v, want 4.8", core)
	}

	// Test querying latest respiratory rate
	rr, err := queryLatestRespiratoryRate(db, today)
	if err != nil {
		t.Errorf("queryLatestRespiratoryRate error: %v", err)
	}
	if rr == nil || *rr != 12.0 {
		t.Errorf("respiratory_rate = %v, want 12.0", rr)
	}
}

// Test HRV-based recovery classification
func TestClassifyRecoveryStatus(t *testing.T) {
	tests := []struct {
		name     string
		hrv      *float64
		expected string
	}{
		{"no HRV data", nil, "UNKNOWN"},
		{"very low HRV (poor recovery)", ptr(15.0), "POOR"},
		{"low HRV boundary", ptr(20.0), "POOR"},
		{"moderate HRV", ptr(35.0), "OK"},
		{"good HRV", ptr(50.0), "GOOD"},
		{"excellent HRV", ptr(80.0), "GOOD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &MorningBriefing{
				Vitals: VitalsData{HRV: tt.hrv},
				Sleep:  SleepData{DataAvailable: false},
			}
			classify(b)
			if b.Classification.RecoveryStatus != tt.expected {
				t.Errorf("RecoveryStatus = %q, want %q", b.Classification.RecoveryStatus, tt.expected)
			}
		})
	}
}

// Test sleep classification with deep sleep factor
func TestClassifySleepWithDeepSleep(t *testing.T) {
	tests := []struct {
		name         string
		totalHours   *float64
		deepHours    *float64
		isCurrentDay bool
		expected     string
	}{
		{"good total, good deep", ptr(7.5), ptr(1.5), true, "GOOD"},
		{"good total, low deep", ptr(7.5), ptr(0.5), true, "OK"}, // Downgraded due to low deep
		{"good total, very low deep", ptr(7.5), ptr(0.3), true, "OK"},
		{"ok total, good deep", ptr(6.0), ptr(1.2), true, "OK"},
		{"ok total, low deep", ptr(6.0), ptr(0.5), true, "POOR"}, // Downgraded due to low deep
		{"poor total, any deep", ptr(4.0), ptr(1.5), true, "POOR"},
		{"no deep data", ptr(7.5), nil, true, "GOOD"}, // Falls back to total-only
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &MorningBriefing{
				Sleep: SleepData{
					TotalHours:    tt.totalHours,
					DeepHours:     tt.deepHours,
					DataAvailable: true,
					IsCurrentDay:  tt.isCurrentDay,
				},
			}
			classify(b)
			if b.Classification.SleepQuality != tt.expected {
				t.Errorf("SleepQuality = %q, want %q", b.Classification.SleepQuality, tt.expected)
			}
		})
	}
}

// Test combined recommendation with recovery status
func TestClassifyRecommendationsWithRecovery(t *testing.T) {
	tests := []struct {
		name         string
		sleepHours   *float64
		deepHours    *float64
		hrv          *float64
		morningCount int
		wantContains string
	}{
		{
			"poor recovery poor sleep",
			ptr(4.0), ptr(0.5), ptr(15.0), 3,
			"recovery",
		},
		{
			"good sleep poor recovery",
			ptr(8.0), ptr(1.5), ptr(18.0), 1,
			"HRV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make([]CalendarEvent, tt.morningCount)
			for i := range events {
				events[i] = CalendarEvent{Time: "09:00", Summary: "Test"}
			}

			b := &MorningBriefing{
				Sleep: SleepData{
					TotalHours:    tt.sleepHours,
					DeepHours:     tt.deepHours,
					DataAvailable: true,
					IsCurrentDay:  true,
				},
				Vitals: VitalsData{HRV: tt.hrv},
				Calendar: CalendarData{
					MorningEvents: events,
					MorningCount:  tt.morningCount,
				},
			}
			classify(b)
			if !contains(b.Classification.Recommendation, tt.wantContains) {
				t.Errorf("Recommendation = %q, want to contain %q", b.Classification.Recommendation, tt.wantContains)
			}
		})
	}
}

// Test JSON output includes all new fields
func TestMorningBriefingJSONOutputWithNewFields(t *testing.T) {
	b := MorningBriefing{
		GeneratedAt: time.Now().Format(time.RFC3339),
		TargetDate:  time.Now().Format("2006-01-02"),
		Sleep: SleepData{
			TotalHours:    ptr(7.5),
			DeepHours:     ptr(1.2),
			REMHours:      ptr(1.5),
			CoreHours:     ptr(4.8),
			DataAvailable: true,
			IsCurrentDay:  true,
		},
		Vitals: VitalsData{
			RestingHR:       ptr(52.0),
			HRV:             ptr(45.0),
			SpO2:            ptr(98.0),
			RespiratoryRate: ptr(12.5),
		},
		Classification: Classification{
			SleepQuality:   "GOOD",
			MorningLoad:    "LIGHT",
			RecoveryStatus: "GOOD",
			Recommendation: "Well rested",
		},
	}

	output, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Verify all new fields are present in JSON
	outputStr := string(output)
	expectedFields := []string{
		`"core_hours"`,
		`"respiratory_rate"`,
		`"recovery_status"`,
	}

	for _, field := range expectedFields {
		if !contains(outputStr, field) {
			t.Errorf("JSON output missing field %s", field)
		}
	}
}
