package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrNotConfigured = errors.New("Instagram veri sağlayıcısı yapılandırılmadı")
var ErrNotFound = errors.New("Instagram kaydı bulunamadı")
var ErrConflict = errors.New("Instagram hesabı zaten takip ediliyor")
var ErrInvalidInput = errors.New("geçersiz Instagram hesabı girdisi")

// ExternalInstagramProvider implements a small, documented adapter protocol. Switching
// vendors only requires another InstagramProvider implementation.
type ExternalInstagramProvider struct {
	baseURL, apiKey string
	http            *http.Client
	mu              sync.Mutex
	lastRequest     time.Time
	minInterval     time.Duration
}

func NewExternalInstagramProvider(baseURL, apiKey string, timeout, minInterval time.Duration) *ExternalInstagramProvider {
	return &ExternalInstagramProvider{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: &http.Client{Timeout: timeout}, minInterval: minInterval}
}
func (p *ExternalInstagramProvider) GetProfile(ctx context.Context, username string) (*Profile, error) {
	var out Profile
	err := p.get(ctx, "/profiles/"+url.PathEscape(cleanUsername(username)), nil, &out)
	return &out, err
}
func (p *ExternalInstagramProvider) GetPosts(ctx context.Context, username, cursor string) (*PostPage, error) {
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out PostPage
	err := p.get(ctx, "/profiles/"+url.PathEscape(cleanUsername(username))+"/posts", q, &out)
	return &out, err
}
func (p *ExternalInstagramProvider) get(ctx context.Context, path string, q url.Values, out any) error {
	if p.baseURL == "" || p.apiKey == "" {
		return ErrNotConfigured
	}
	endpoint := p.baseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	var last error
	for attempt := 1; attempt <= 4; attempt++ {
		if err := p.throttle(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("X-API-Key", p.apiKey)
		req.Header.Set("Accept", "application/json")
		res, err := p.http.Do(req)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(res.Body, 8<<20))
			res.Body.Close()
			if readErr != nil {
				return readErr
			}
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				if err := json.Unmarshal(body, out); err != nil {
					return fmt.Errorf("Instagram provider yanıtı çözülemedi: %w", err)
				}
				return nil
			}
			if res.StatusCode == http.StatusNotFound {
				return ErrNotFound
			}
			last = fmt.Errorf("provider HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
			if res.StatusCode != http.StatusTooManyRequests && res.StatusCode < 500 {
				return last
			}
		} else {
			last = err
		}
		log.Printf("instagram provider retry path=%s attempt=%d error=%v", path, attempt, last)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return fmt.Errorf("Instagram provider isteği başarısız: %w", last)
}
func (p *ExternalInstagramProvider) throttle(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	wait := p.minInterval - time.Since(p.lastRequest)
	if wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	p.lastRequest = time.Now()
	return nil
}
func cleanUsername(v string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(v), "@"))
}
