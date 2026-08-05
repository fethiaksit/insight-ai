package instagram

import (
	"context"
	"time"
)

// InstagramProvider is the only contract the collector knows about. Concrete
// provider response formats are translated into these provider-neutral types.
type InstagramProvider interface {
	GetProfile(ctx context.Context, username string) (*Profile, error)
	GetPosts(ctx context.Context, username string, cursor string) (*PostPage, error)
}

type Profile struct {
	Username          string `json:"username"`
	Name              string `json:"name,omitempty"`
	ProfilePictureURL string `json:"profile_picture_url,omitempty"`
}

type Post struct {
	Platform     string    `json:"platform"`
	Username     string    `json:"username"`
	ExternalID   string    `json:"external_id"`
	Shortcode    string    `json:"shortcode"`
	Caption      string    `json:"caption"`
	Permalink    string    `json:"permalink"`
	MediaType    string    `json:"media_type"`
	MediaURL     string    `json:"media_url"`
	ThumbnailURL string    `json:"thumbnail_url"`
	PublishedAt  time.Time `json:"published_at"`
	CollectedAt  time.Time `json:"collected_at"`
}

type PostPage struct {
	Posts      []Post `json:"posts"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type Account struct {
	ID                string     `json:"id"`
	Username          string     `json:"username"`
	DisplayName       string     `json:"display_name,omitempty"`
	ProfileURL        string     `json:"profile_url"`
	ProfilePictureURL string     `json:"profile_picture_url,omitempty"`
	Active            bool       `json:"active"`
	LastSyncAt        *time.Time `json:"last_sync_at,omitempty"`
	TotalPosts        int64      `json:"total_posts"`
	SyncError         string     `json:"sync_error,omitempty"`
	SyncStatus        string     `json:"sync_status"`
	CreatedAt         time.Time  `json:"created_at"`
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
