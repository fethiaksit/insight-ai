package instagram

import (
	"context"
	"errors"
	"time"
)

type Collector struct {
	provider InstagramProvider
	repo     *Repository
}

func NewCollector(provider InstagramProvider, repo *Repository) *Collector {
	return &Collector{provider: provider, repo: repo}
}

func (c *Collector) Sync(ctx context.Context, username string, fullHistory bool, onEvent func(InstagramScrapeEvent)) (int, error) {
	if c.provider == nil {
		return 0, ErrNotConfigured
	}
	username = cleanUsername(username)
	if username == "" {
		return 0, errors.New("Instagram kullanıcı adı gerekli")
	}
	known, e := c.repo.KnownShortcodes(ctx, username, 50)
	if e != nil {
		return 0, e
	}
	events, errs := c.provider.ScrapeProfile(ctx, username, known, fullHistory)
	total := 0
	for events != nil || errs != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if onEvent != nil {
				onEvent(ev)
			}
			if ev.Profile != nil {
				a, accountErr := c.repo.Account(ctx, username)
				if accountErr == nil {
					a.DisplayName = ev.Profile.FullName
					a.ProfilePictureURL = ev.Profile.ProfilePicURL
					a.UpdatedAt = syncTime()
					if saveErr := c.repo.SaveAccount(ctx, a); saveErr != nil {
						return total, saveErr
					}
				}
			}
			if ev.Post != nil {
				ev.Post.Username = username
				n, saveErr := c.repo.SavePosts(ctx, []Post{*ev.Post})
				total += n
				if saveErr != nil {
					return total, saveErr
				}
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				return total, err
			}
		case <-ctx.Done():
			return total, ctx.Err()
		}
	}
	return total, nil
}

func syncTime() time.Time { return time.Now().UTC() }
