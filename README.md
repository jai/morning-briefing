# Briefing

Deterministic data aggregation for daily health briefings. Collects data from multiple sources and outputs structured JSON for LLM interpretation.

## Purpose

This tool gathers health, calendar, medication, and training data from various sources and produces a single JSON output suitable for an AI health assistant to interpret and present as a morning or evening briefing.

## Modes

```bash
briefing              # Default: morning briefing
briefing --morning    # Explicit morning mode
briefing --evening    # Evening wrap-up
```

## Data Sources

| Source | Tool | Data |
|--------|------|------|
| Apple Health | `health-ingest` | Sleep (total, deep, REM), vitals (RHR, HRV, SpO2), active energy, dietary energy, protein, steps |
| Google Calendar | `gog` | Today's events (personal + work calendars) |
| Todoist | `td` | Medication tasks (💊Meds and 💉 labels) |
| Hevy | `mcporter` | Recent workouts, training frequency |

## Morning Output

```json
{
  "generated_at": "2024-01-15T07:30:00+07:00",
  "target_date": "2024-01-15",
  "sleep": {
    "total_hours": 7.5,
    "deep_hours": 1.2,
    "rem_hours": 1.8,
    "data_available": true,
    "is_current_day": true
  },
  "vitals": {
    "resting_hr_bpm": 52,
    "hrv_ms": 45,
    "spo2_pct": 98
  },
  "calendar": {
    "morning_events": [...],
    "afternoon_events": [...],
    "morning_count": 2,
    "first_event_time": "09:00"
  },
  "meds": {
    "due_today": [...],
    "overdue": [...],
    "completed": [...]
  },
  "training": {
    "last_workout": {...},
    "days_since_last": 1,
    "weekly_count": 5
  },
  "classification": {
    "sleep_quality": "GOOD",
    "morning_load": "LIGHT",
    "recommendation": "Well rested. Attack the day."
  }
}
```

## Evening Output

```json
{
  "mode": "evening",
  "generated_at": "...",
  "target_date": "2026-02-03",
  "energy": {
    "deficit_or_surplus_kcal": -400,
    "status": "deficit",
    "bmr_kcal": 1636,
    "active_kcal": 611,
    "total_burned_kcal": 2247,
    "consumed_kcal": 1850
  },
  "protein": {
    "consumed_g": 128,
    "target_g": 152,
    "remaining_g": 24,
    "on_track": false
  },
  "activity": {
    "steps": 8432,
    "workout": { "done": true, "title": "Arms", "duration": "32m" },
    "stand_hours": 10
  },
  "recovery": {
    "hrv_ms": 45,
    "hrv_yesterday_ms": 38,
    "resting_hr_bpm": 68,
    "sleep_last_night": { "total_hrs": 5.4, "deep_hrs": 0.56 }
  },
  "protocols": {
    "completed": ["T + HCG", "TB-500", "Retatrutide"],
    "missed": ["PrEP", "Nexium"]
  },
  "tomorrow": {
    "first_event": { "time": "08:00", "summary": "Workout" },
    "workout_scheduled": true,
    "meds_due": ["Testosterone (Fri AM)"]
  }
}
```

## Classification Logic

**Sleep Quality:**
- `GOOD`: ≥7 hours
- `OK`: 5-7 hours  
- `POOR`: <5 hours
- `UNKNOWN`: No data or stale data

**Morning Load:**
- `CLEAR`: 0 morning events
- `LIGHT`: 1-2 morning events
- `PACKED`: 3+ morning events

## Usage

```bash
# Build
go build -o briefing

# Run morning briefing (default)
./briefing

# Run evening wrap-up
./briefing --evening

# Pipe to jq for pretty output
./briefing | jq .
./briefing --evening | jq .

# Use in scripts
./briefing > /tmp/briefing.json
```

## Requirements

The following CLI tools must be available in PATH:

- `health-ingest` - Apple Health data via [health-ingest](https://github.com/jai/health-ingest)
- `gog` - Google Calendar CLI
- `td` - Todoist CLI
- `mcporter` - MCP client for Hevy integration

## Installation

```bash
go install github.com/jai/briefing@latest
```

Or build from source:

```bash
git clone https://github.com/jai/briefing.git
cd briefing
go build -o briefing
```

## License

MIT
