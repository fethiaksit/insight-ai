package instagram

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type InstaloaderHTTPProvider struct {
	baseURL string
	client  *http.Client
}

func NewInstaloaderHTTPProvider(baseURL string, timeout time.Duration) *InstaloaderHTTPProvider {
	return &InstaloaderHTTPProvider{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: timeout}}
}

func (p *InstaloaderHTTPProvider) ScrapeProfile(ctx context.Context, username string, known []string) (<-chan InstagramScrapeEvent, <-chan error) {
	events, errs := make(chan InstagramScrapeEvent), make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		body, _ := json.Marshal(map[string]any{"profile": username, "known_shortcodes": known, "max_posts": 0})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/profiles/scrape", bytes.NewReader(body))
		if err != nil {
			errs <- err
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/x-ndjson")
		res, err := p.client.Do(req)
		if err != nil {
			errs <- fmt.Errorf("Instagram scraper bağlantı hatası: %w", err)
			return
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			errs <- fmt.Errorf("Instagram scraper HTTP %d", res.StatusCode)
			return
		}
		scanner := bufio.NewScanner(res.Body)
		scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			var raw struct {
				Type    string `json:"type"`
				Profile *struct {
					Username      string `json:"username"`
					FullName      string `json:"full_name"`
					ProfilePicURL string `json:"profile_pic_url"`
					IsPrivate     bool   `json:"is_private"`
					PostsCount    int64  `json:"posts_count"`
				} `json:"profile"`
				Post    *Post                           `json:"post"`
				Fetched int                             `json:"fetched"`
				Stopped bool                            `json:"stopped_on_known_post"`
				Error   *struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
				errs <- fmt.Errorf("scraper NDJSON satırı geçersiz: %w", err)
				return
			}
			if raw.Type == "error" && raw.Error != nil {
				errs <- fmt.Errorf("%s: %s", raw.Error.Code, raw.Error.Message)
				return
			}
			ev := InstagramScrapeEvent{Type: raw.Type, Post: raw.Post}
			if raw.Profile != nil {
				ev.Profile = &ScrapedProfile{Username: raw.Profile.Username, FullName: raw.Profile.FullName, ProfilePicURL: raw.Profile.ProfilePicURL, IsPrivate: raw.Profile.IsPrivate, PostsCount: raw.Profile.PostsCount}
			}
			if raw.Type == "complete" {
				ev.Complete = &ScrapeComplete{Fetched: raw.Fetched, StoppedOnKnownPost: raw.Stopped}
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errs <- fmt.Errorf("scraper akışı okunamadı: %w", err)
		}
	}()
	return events, errs
}
