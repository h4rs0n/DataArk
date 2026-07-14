package observability

import (
	"encoding/json"
	"log"
	"time"
)

// Event is deliberately identifier-only. It cannot carry article bodies,
// cookies, authorization headers, access tokens, or model credentials.
type Event struct {
	Name        string    `json:"event"`
	OccurredAt  time.Time `json:"occurred_at"`
	JobID       string    `json:"job_id,omitempty"`
	FetchRunID  uint      `json:"fetch_run_id,omitempty"`
	SiteID      uint      `json:"site_id,omitempty"`
	SourceID    uint      `json:"source_id,omitempty"`
	CandidateID uint      `json:"candidate_id,omitempty"`
	UserID      uint      `json:"user_id,omitempty"`
	DayID       uint      `json:"day_id,omitempty"`
	LocalDate   string    `json:"local_date,omitempty"`
	Status      string    `json:"status,omitempty"`
	ErrorType   string    `json:"error_type,omitempty"`
	Count       int       `json:"count,omitempty"`
}

func Log(event Event) {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	log.Printf("dataark_event %s", payload)
}
