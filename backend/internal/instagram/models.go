package instagram

import (
	"context"
	"time"
)

// InstagramProvider is the only contract the collector knows about. Concrete
// provider response formats are translated into these provider-neutral types.
type InstagramProvider interface {
	ScrapeProfile(ctx context.Context, username string, knownShortcodes []string, fullHistory bool) (<-chan InstagramScrapeEvent, <-chan error)
}

type InstagramScrapeEvent struct {
	Type     string          `json:"type"`
	Profile  *ScrapedProfile `json:"profile,omitempty"`
	Post     *Post           `json:"post,omitempty"`
	Complete *ScrapeComplete `json:"complete,omitempty"`
	Progress *ScrapeProgress `json:"progress,omitempty"`
}
type ScrapeProgress struct {
	Discovered, Processed, Saved, Updated, Skipped, Failed, ScrollRound int
	Status                                                              string
}
type ScrapedProfile struct {
	Username, FullName, ProfilePicURL string
	IsPrivate                         bool
	PostsCount                        int64
}
type ScrapeComplete struct {
	Fetched            int  `json:"fetched"`
	StoppedOnKnownPost bool `json:"stopped_on_known_post"`
}

type Profile struct {
	Username          string `json:"username"`
	Name              string `json:"name,omitempty"`
	ProfilePictureURL string `json:"profile_picture_url,omitempty"`
}

type Post struct {
	Platform      string    `json:"platform"`
	Username      string    `json:"username"`
	ExternalID    string    `json:"external_id"`
	Shortcode     string    `json:"shortcode"`
	Caption       string    `json:"caption"`
	Permalink     string    `json:"permalink"`
	MediaType     string    `json:"media_type"`
	MediaURL      string    `json:"media_url"`
	ThumbnailURL  string    `json:"thumbnail_url"`
	IsVideo       bool      `json:"is_video"`
	IsPinned      bool      `json:"is_pinned"`
	LikesCount    int64     `json:"likes_count"`
	CommentsCount int64     `json:"comments_count"`
	PublishedAt   time.Time `json:"published_at"`
	CollectedAt   time.Time `json:"collected_at"`
}

type PostPage struct {
	Posts      []Post `json:"posts"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type Account struct {
	ID                string      `json:"id"`
	Username          string      `json:"username"`
	DisplayName       string      `json:"display_name,omitempty"`
	ProfileURL        string      `json:"profile_url"`
	ProfilePictureURL string      `json:"profile_picture_url,omitempty"`
	Active            bool        `json:"active"`
	LastSyncAt        *time.Time  `json:"last_sync_at,omitempty"`
	TotalPosts        int64       `json:"total_posts"`
	SyncError         string      `json:"sync_error,omitempty"`
	SyncStatus        string      `json:"sync_status"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
	CooldownUntil     *time.Time  `json:"cooldown_until,omitempty"`
	LastSyncResult    *SyncResult `json:"last_sync_result,omitempty"`
}
type SyncResult struct {
	NewPosts     int       `json:"new_posts"`
	UpdatedPosts int       `json:"updated_posts"`
	SkippedPosts int       `json:"skipped_posts"`
	Provider     string    `json:"provider"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	Error        string    `json:"error,omitempty"`
}
type FullSyncState struct {
	JobID             string     `json:"job_id"`
	Username          string     `json:"username"`
	Status            string     `json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	DiscoveredCount   int        `json:"discovered_count"`
	ProcessedCount    int        `json:"processed_count"`
	SavedCount        int        `json:"saved_count"`
	UpdatedCount      int        `json:"updated_count"`
	SkippedCount      int        `json:"skipped_count"`
	FailedCount       int        `json:"failed_count"`
	ScrollRound       int        `json:"scroll_round"`
	NoNewRounds       int        `json:"no_new_rounds"`
	LastShortcode     string     `json:"last_shortcode,omitempty"`
	OldestShortcode   string     `json:"oldest_shortcode,omitempty"`
	OldestPublishedAt string     `json:"oldest_published_at,omitempty"`
	LastScrollHeight  int64      `json:"last_scroll_height"`
	PauseReason       string     `json:"pause_reason,omitempty"`
	RetryAfter        int        `json:"retry_after"`
	CancelRequested   bool       `json:"cancel_requested"`
	Error             string     `json:"error,omitempty"`
}

type Status struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider,omitempty"`
	Demo       bool   `json:"demo"`
	TotalPosts int64  `json:"total_posts"`
}

type ListOptions struct {
	Page, Limit        int
	Search, Username   string
	Match, MediaType   string
	StartDate, EndDate *time.Time
	Sort               string
}
type ListResult struct {
	Data []Post `json:"data"`
	Meta Meta   `json:"meta"`
}
type Meta struct {
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}
