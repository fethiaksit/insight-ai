package instagram

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
func (p *InstaloaderHTTPProvider) Browser(ctx context.Context, method, path string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	res, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("scraper HTTP %d", res.StatusCode)
	}
	return out, nil
}

func (p *InstaloaderHTTPProvider) ScrapeProfile(ctx context.Context, username string, known []string) (<-chan InstagramScrapeEvent, <-chan error) {
	events, errs := make(chan InstagramScrapeEvent), make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		maxPosts := 20
		if len(known) > 0 {
			maxPosts = 10
		}
		body, _ := json.Marshal(map[string]any{"profile": username, "known_shortcodes": known, "max_posts": maxPosts})
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
				log.Printf("scraper NDJSON satırı atlandı: %v", err)
				continue
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
