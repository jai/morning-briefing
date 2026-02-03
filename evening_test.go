package main

import (
	"encoding/json"
	"testing"
	"time"
)

// ==================== BMR CALCULATION TESTS ====================

// Test BMR calculation using Mifflin-St Jeor formula
// Men: BMR = (10 × weight in kg) + (6.25 × height in cm) - (5 × age) + 5
func TestCalculateBMR(t *testing.T) {
	tests := []struct {
		name     string
		weight   float64 // kg
		height   float64 // cm
		age      int
		male     bool
		expected int // kcal
	}{
		{
			name:     "Jai's stats (41yo, 73kg, 177cm, male)",
			weight:   73,
			height:   177,
			age:      41,
			male:     true,
			expected: 1636, // (10×73) + (6.25×177) - (5×41) + 5 = 730 + 1106.25 - 205 + 5 = 1636.25
		},
		{
			name:     "Edge case: younger male",
			weight:   80,
			height:   180,
			age:      25,
			male:     true,
			expected: 1805, // (10×80) + (6.25×180) - (5×25) + 5 = 800 + 1125 - 125 + 5 = 1805
		},
		{
			name:     "Female calculation",
			weight:   60,
			height:   165,
			age:      30,
			male:     false,
			expected: 1320, // (10×60) + (6.25×165) - (5×30) - 161 = 600 + 1031.25 - 150 - 161 = 1320.25
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateBMR(tt.weight, tt.height, tt.age, tt.male)
			// Allow ±1 kcal tolerance for rounding
			if result < tt.expected-1 || result > tt.expected+1 {
				t.Errorf("CalculateBMR(%v, %v, %v, %v) = %d, want %d", tt.weight, tt.height, tt.age, tt.male, result, tt.expected)
			}
		})
	}
}

// ==================== DEFICIT/SURPLUS CALCULATION TESTS ====================

func TestCalculateEnergyBalance(t *testing.T) {
	tests := []struct {
		name               string
		bmr                int
		activeEnergy       float64
		consumedEnergy     float64
		expectedBalance    int
		expectedStatus     string // "deficit" or "surplus"
	}{
		{
			name:               "Caloric deficit",
			bmr:                1636,
			activeEnergy:       611,
			consumedEnergy:     1850,
			expectedBalance:    -397, // 1850 - (1636 + 611) = 1850 - 2247 = -397
			expectedStatus:     "deficit",
		},
		{
			name:               "Caloric surplus",
			bmr:                1636,
			activeEnergy:       400,
			consumedEnergy:     2500,
			expectedBalance:    464, // 2500 - (1636 + 400) = 2500 - 2036 = 464
			expectedStatus:     "surplus",
		},
		{
			name:               "Maintenance (within 50 kcal)",
			bmr:                1636,
			activeEnergy:       500,
			consumedEnergy:     2136,
			expectedBalance:    0,
			expectedStatus:     "maintenance",
		},
		{
			name:               "Zero active energy",
			bmr:                1636,
			activeEnergy:       0,
			consumedEnergy:     1200,
			expectedBalance:    -436,
			expectedStatus:     "deficit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balance, status := CalculateEnergyBalance(tt.bmr, tt.activeEnergy, tt.consumedEnergy)
			// Allow ±5 kcal tolerance
			if balance < tt.expectedBalance-5 || balance > tt.expectedBalance+5 {
				t.Errorf("CalculateEnergyBalance() balance = %d, want %d", balance, tt.expectedBalance)
			}
			if status != tt.expectedStatus {
				t.Errorf("CalculateEnergyBalance() status = %q, want %q", status, tt.expectedStatus)
			}
		})
	}
}

// ==================== PROTEIN REMAINING CALCULATION TESTS ====================

func TestCalculateProteinRemaining(t *testing.T) {
	tests := []struct {
		name             string
		consumed         float64
		target           float64
		expectedRemain   float64
		expectedOnTrack  bool
	}{
		{
			name:             "Under target",
			consumed:         128,
			target:           152,
			expectedRemain:   24,
			expectedOnTrack:  false,
		},
		{
			name:             "At target",
			consumed:         152,
			target:           152,
			expectedRemain:   0,
			expectedOnTrack:  true,
		},
		{
			name:             "Over target",
			consumed:         170,
			target:           152,
			expectedRemain:   0,
			expectedOnTrack:  true,
		},
		{
			name:             "Close to target (95%)",
			consumed:         144.4, // 95% of 152
			target:           152,
			expectedRemain:   7.6,
			expectedOnTrack:  true, // 95%+ is on track
		},
		{
			name:             "Zero consumed",
			consumed:         0,
			target:           152,
			expectedRemain:   152,
			expectedOnTrack:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remaining, onTrack := CalculateProteinStatus(tt.consumed, tt.target)
			// Allow small tolerance for floating point
			if remaining < tt.expectedRemain-0.5 || remaining > tt.expectedRemain+0.5 {
				t.Errorf("CalculateProteinStatus() remaining = %.1f, want %.1f", remaining, tt.expectedRemain)
			}
			if onTrack != tt.expectedOnTrack {
				t.Errorf("CalculateProteinStatus() onTrack = %v, want %v", onTrack, tt.expectedOnTrack)
			}
		})
	}
}

// ==================== CLI FLAG PARSING TESTS ====================

func TestParseMode(t *testing.T) {
	tests := []struct {
		name         string
		morning      bool
		evening      bool
		expectedMode string
		expectError  bool
	}{
		{
			name:         "No flags (default morning)",
			morning:      false,
			evening:      false,
			expectedMode: "morning",
			expectError:  false,
		},
		{
			name:         "Explicit morning",
			morning:      true,
			evening:      false,
			expectedMode: "morning",
			expectError:  false,
		},
		{
			name:         "Explicit evening",
			morning:      false,
			evening:      true,
			expectedMode: "evening",
			expectError:  false,
		},
		{
			name:         "Both flags (error)",
			morning:      true,
			evening:      true,
			expectedMode: "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := ParseMode(tt.morning, tt.evening)
			if tt.expectError {
				if err == nil {
					t.Errorf("ParseMode() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("ParseMode() unexpected error: %v", err)
				}
				if mode != tt.expectedMode {
					t.Errorf("ParseMode() = %q, want %q", mode, tt.expectedMode)
				}
			}
		})
	}
}

// ==================== EVENING OUTPUT STRUCTURE TESTS ====================

func TestEveningBriefingStructure(t *testing.T) {
	now := time.Now()
	today := now.Format("2006-01-02")

	eb := EveningBriefing{
		Mode:        "evening",
		GeneratedAt: now.Format(time.RFC3339),
		TargetDate:  today,
		Energy: EnergyData{
			DeficitOrSurplusKcal: -400,
			Status:               "deficit",
			BMRKcal:              1636,
			ActiveKcal:           611,
			TotalBurnedKcal:      2247,
			ConsumedKcal:         1850,
		},
		Protein: ProteinData{
			ConsumedG:  128,
			TargetG:    152,
			RemainingG: 24,
			OnTrack:    false,
		},
		Activity: ActivityData{
			Steps: 8432,
			Workout: &WorkoutInfo{
				Done:     true,
				Title:    "Arms",
				Duration: "32m",
			},
			StandHours: 10,
		},
		Recovery: RecoveryData{
			HRVMS:          45,
			HRVYesterdayMS: 38,
			RestingHRBPM:   68,
			SleepLastNight: SleepInfo{
				TotalHrs: 5.4,
				DeepHrs:  0.56,
			},
		},
		Protocols: ProtocolsData{
			Completed: []string{"T + HCG", "TB-500", "Retatrutide"},
			Missed:    []string{"PrEP", "Nexium"},
		},
		Tomorrow: TomorrowData{
			FirstEvent: &EventInfo{
				Time:    "08:00",
				Summary: "Workout",
			},
			WorkoutScheduled: true,
			MedsDue:          []string{"Testosterone (Fri AM)"},
		},
	}

	// Marshal to JSON
	output, err := json.MarshalIndent(eb, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal EveningBriefing: %v", err)
	}

	// Verify it unmarshals back correctly
	var parsed EveningBriefing
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal EveningBriefing: %v", err)
	}

	// Verify key fields
	if parsed.Mode != "evening" {
		t.Errorf("Mode = %q, want %q", parsed.Mode, "evening")
	}
	if parsed.Energy.Status != "deficit" {
		t.Errorf("Energy.Status = %q, want %q", parsed.Energy.Status, "deficit")
	}
	if parsed.Energy.DeficitOrSurplusKcal != -400 {
		t.Errorf("Energy.DeficitOrSurplusKcal = %d, want %d", parsed.Energy.DeficitOrSurplusKcal, -400)
	}
	if parsed.Protein.RemainingG != 24 {
		t.Errorf("Protein.RemainingG = %.0f, want %d", parsed.Protein.RemainingG, 24)
	}
	if !parsed.Activity.Workout.Done {
		t.Error("Activity.Workout.Done = false, want true")
	}
	if len(parsed.Protocols.Completed) != 3 {
		t.Errorf("len(Protocols.Completed) = %d, want %d", len(parsed.Protocols.Completed), 3)
	}
	if parsed.Tomorrow.FirstEvent.Time != "08:00" {
		t.Errorf("Tomorrow.FirstEvent.Time = %q, want %q", parsed.Tomorrow.FirstEvent.Time, "08:00")
	}
}

// Test EveningBriefing JSON field names match spec
func TestEveningBriefingJSONFieldNames(t *testing.T) {
	eb := EveningBriefing{
		Mode:        "evening",
		GeneratedAt: time.Now().Format(time.RFC3339),
		TargetDate:  "2026-02-03",
		Energy: EnergyData{
			DeficitOrSurplusKcal: -400,
			Status:               "deficit",
			BMRKcal:              1636,
			ActiveKcal:           611,
			TotalBurnedKcal:      2247,
			ConsumedKcal:         1850,
		},
		Protein: ProteinData{
			ConsumedG:  128,
			TargetG:    152,
			RemainingG: 24,
			OnTrack:    false,
		},
	}

	output, _ := json.Marshal(eb)
	outputStr := string(output)

	// Check required JSON field names
	requiredFields := []string{
		`"mode"`,
		`"generated_at"`,
		`"target_date"`,
		`"deficit_or_surplus_kcal"`,
		`"status"`,
		`"bmr_kcal"`,
		`"active_kcal"`,
		`"total_burned_kcal"`,
		`"consumed_kcal"`,
		`"consumed_g"`,
		`"target_g"`,
		`"remaining_g"`,
		`"on_track"`,
	}

	for _, field := range requiredFields {
		if !contains(outputStr, field) {
			t.Errorf("JSON output missing required field %s", field)
		}
	}
}

// ==================== USER STATS CONSTANTS TESTS ====================

func TestUserStatsConstants(t *testing.T) {
	// Verify the user stats are correctly defined
	if UserAge != 41 {
		t.Errorf("UserAge = %d, want %d", UserAge, 41)
	}
	if UserWeightKg != 73 {
		t.Errorf("UserWeightKg = %.0f, want %d", UserWeightKg, 73)
	}
	if UserHeightCm != 177 {
		t.Errorf("UserHeightCm = %.0f, want %d", UserHeightCm, 177)
	}
	if UserIsMale != true {
		t.Error("UserIsMale = false, want true")
	}
	if UserProteinTargetG != 152 {
		t.Errorf("UserProteinTargetG = %d, want %d", UserProteinTargetG, 152)
	}

	// Verify BMR calculation matches expected
	calculatedBMR := CalculateBMR(UserWeightKg, UserHeightCm, UserAge, UserIsMale)
	if calculatedBMR != UserBMRKcal {
		t.Errorf("Calculated BMR = %d, but UserBMRKcal constant = %d", calculatedBMR, UserBMRKcal)
	}
}
