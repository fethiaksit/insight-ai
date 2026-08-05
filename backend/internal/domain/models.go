package domain

import (
	"time"

	"github.com/google/uuid"
)

type Platform string

const (
	PlatformX         Platform = "x"
	PlatformInstagram Platform = "instagram"
	PlatformYouTube   Platform = "youtube"
	PlatformRSS       Platform = "rss"
)

// Account is stored as one JSON document in Redis.
type Account struct {
	ID            uuid.UUID  `json:"id"`
	Platform      Platform   `json:"platform"`
	Username      string     `json:"username"`
	ProfileName   string     `json:"profileName"`
	Active        bool       `json:"active"`
	LastScannedAt *time.Time `json:"lastScannedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type Post struct {
	ID          uuid.UUID   `json:"id"`
	AccountID   uuid.UUID   `json:"accountId"`
	ExternalID  string      `json:"externalId"`
	Content     string      `json:"content"`
	URL         string      `json:"url"`
	PostType    string      `json:"postType"`
	PublishedAt time.Time   `json:"publishedAt"`
	CreatedAt   time.Time   `json:"createdAt"`
	Account     Account     `json:"account,omitempty"`
	Analysis    *AIAnalysis `json:"analysis,omitempty"`
}

type AIAnalysis struct {
	ID         uuid.UUID `json:"id"`
	PostID     uuid.UUID `json:"postId"`
	Summary    string    `json:"summary"`
	MainTopic  string    `json:"mainTopic"`
	SubTopic   string    `json:"subTopic"`
	Keywords   []string  `json:"keywords"`
	Sentiment  string    `json:"sentiment"`
	IsRelevant bool      `json:"isRelevant"`
	Confidence float64   `json:"confidence"`
	Embedding  []float32 `json:"-"`
	AnalyzedAt time.Time `json:"analyzedAt"`
}

type Topic struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Settings and keyword data are persisted as Redis JSON documents as well.
type Settings struct {
	ID        string    `json:"id"`
	Keywords  []string  `json:"keywords"`
	UpdatedAt time.Time `json:"updatedAt"`
}
