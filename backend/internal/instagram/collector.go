package instagram

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Collector struct {
	provider InstagramProvider
	repo     *Repository
	maxPages int
}

func NewCollector(provider InstagramProvider, repo *Repository) *Collector {
	return &Collector{provider: provider, repo: repo, maxPages: 1000}
}

func (c *Collector) Sync(ctx context.Context, username string) (int, error) {
	if c.provider == nil {
		return 0, ErrNotConfigured
	}
	username = cleanUsername(username)
	if username == "" {
		return 0, errors.New("Instagram kullanıcı adı gerekli")
	}
	cursor, seen, total := "", map[string]bool{}, 0
	for page := 0; page < c.maxPages; page++ {
		result, e := c.provider.GetPosts(ctx, username, cursor)
		if e != nil {
			return total, fmt.Errorf("@%s sayfa %d: %w", username, page+1, e)
		}
		if result == nil {
			return total, errors.New("Instagram provider boş sayfa döndürdü")
		}
		for i := range result.Posts {
			result.Posts[i].Username = username
		}
		created, e := c.repo.SavePosts(ctx, result.Posts)
		if e != nil {
			return total, e
		}
		total += created
		if result.NextCursor == "" {
			return total, nil
		}
		if seen[result.NextCursor] {
			return total, errors.New("Instagram provider aynı pagination cursor değerini tekrarladı")
		}
		seen[result.NextCursor] = true
		cursor = result.NextCursor
	}
	return total, errors.New("Instagram provider pagination güvenlik sınırını aştı")
}

func syncTime() time.Time { return time.Now().UTC() }
