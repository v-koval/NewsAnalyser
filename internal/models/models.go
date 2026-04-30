package models

import "time"

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Settings struct {
	CursorAPIKey     string `json:"cursor_api_key"`
	CursorRepository string `json:"cursor_repository"`
	SMTPHost         string `json:"smtp_host"`
	SMTPPort         int    `json:"smtp_port"`
	SMTPUser         string `json:"smtp_user"`
	SMTPPassword     string `json:"smtp_password"`
	SMTPFrom         string `json:"smtp_from"`
	SMTPTLS          bool   `json:"smtp_tls"`
	ProcessingPaused bool   `json:"processing_paused"`
}

type Digest struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Topic          string     `json:"topic"`
	Sources        []string   `json:"sources"`
	IgnoredSources []string   `json:"ignored_sources"`
	FrequencyHours int        `json:"frequency_hours"`
	Recipients     []string   `json:"recipients"`
	Language       string     `json:"language"`
	Kind           string     `json:"kind"`
	Enabled        bool       `json:"enabled"`
	LastRunAt      *time.Time `json:"last_run_at"`
	NextRunAt      *time.Time `json:"next_run_at"`
	AutoSources    []string   `json:"auto_sources"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Material struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	SummaryTitle string `json:"summary_title"`
	SummaryText  string `json:"summary_text"`
	FullText     string `json:"full_text"`
	ImageURL     string `json:"image_url"`
	LocalImage   string `json:"local_image"`
	Position     int    `json:"position"`
}

type DigestRun struct {
	ID              string     `json:"id"`
	DigestID        string     `json:"digest_id"`
	DigestName      string     `json:"digest_name"`
	AnalyzedSources []string   `json:"analyzed_sources"`
	ProcessedAt     time.Time  `json:"processed_at"`
	PeriodFrom      time.Time  `json:"period_from"`
	PeriodTo        time.Time  `json:"period_to"`
	HTML            string     `json:"html,omitempty"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty"`
	Materials       []Material `json:"materials,omitempty"`
}
