package instagram

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"log"
	"strings"
	"sync"
	"time"
)

type Service struct {
	provider     InstagramProvider
	providerName string
	repo         *Repository
	collector    *Collector
	syncing      map[string]bool
	mu           sync.Mutex
	fullCancels  map[string]context.CancelFunc
}

func NewService(provider InstagramProvider, providerName string, repo *Repository) *Service {
	return &Service{provider: provider, providerName: providerName, repo: repo, collector: NewCollector(provider, repo), syncing: map[string]bool{}, fullCancels: map[string]context.CancelFunc{}}
}
func (s *Service) Configured() bool { return s.provider != nil }
func (s *Service) Status(ctx context.Context) (Status, error) {
	total, e := s.repo.TotalPosts(ctx)
	return Status{Configured: s.Configured(), Provider: s.providerName, Demo: s.providerName == "mock" && s.Configured(), TotalPosts: total}, e
}
func (s *Service) Accounts(ctx context.Context) ([]Account, error) { return s.repo.Accounts(ctx) }
func (s *Service) AddAccount(ctx context.Context, username string) (Account, error) {
	username, normalizeErr := NormalizeProfile(username)
	if normalizeErr != nil {
		return Account{}, normalizeErr
	}
	if existing, e := s.repo.Account(ctx, username); e == nil {
		return existing, nil
	} else if !errors.Is(e, ErrNotFound) {
		return Account{}, e
	}
	now := syncTime()
	a := Account{ID: username, Username: username, DisplayName: "@" + username, ProfileURL: canonicalProfileURL(username), Active: true, CreatedAt: now, UpdatedAt: now, SyncStatus: "pending"}
	if e := s.repo.AddAccount(ctx, a); e != nil {
		return Account{}, e
	}
	return a, nil
}
func (s *Service) StartSync(username string) {
	if !s.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if _, e := s.Sync(ctx, username); e != nil {
			log.Printf("instagram background sync failed username=%s error=%v", username, e)
		}
	}()
}
func (s *Service) UpdateAccount(ctx context.Context, id string, username *string, active *bool) (Account, error) {
	a, e := s.repo.Account(ctx, id)
	if e != nil {
		return a, e
	}
	if active != nil {
		a.Active = *active
	}
	if username != nil {
		a.Username, e = NormalizeProfile(*username)
		if e != nil {
			return Account{}, e
		}
		a.ID, a.ProfileURL = a.Username, canonicalProfileURL(a.Username)
	}
	e = s.repo.UpdateAccount(ctx, id, a)
	return a, e
}
func (s *Service) DeleteAccount(ctx context.Context, id string) error {
	return s.repo.DeleteAccount(ctx, id)
}
func (s *Service) Sync(ctx context.Context, username string) (int, error) {
	a, e := s.repo.Account(ctx, username)
	if e != nil {
		return 0, e
	}
	return s.sync(ctx, &a)
}
func (s *Service) sync(ctx context.Context, a *Account) (int, error) {
	started := syncTime()
	if a.CooldownUntil != nil && started.Before(*a.CooldownUntil) {
		return 0, fmt.Errorf("Instagram geçici olarak çok fazla istek algıladı. %d saniye sonra tekrar deneyin", int(time.Until(*a.CooldownUntil).Seconds()))
	}
	// Başarısız senkronizasyonlar kullanıcı düzelttikten (ör. tarayıcıya giriş
	// yaptıktan) hemen sonra yeniden denenebilmelidir. Minimum aralık yalnızca
	// son başarılı senkronizasyon için uygulanır.
	if a.SyncStatus == "completed" && a.LastSyncAt != nil && started.Sub(*a.LastSyncAt) < 15*time.Minute {
		return 0, fmt.Errorf("Bu hesap kısa süre önce senkronize edildi. 15 dakika sonra tekrar deneyin")
	}
	s.mu.Lock()
	if s.syncing[a.Username] {
		s.mu.Unlock()
		return 0, fmt.Errorf("@%s için senkronizasyon zaten çalışıyor", a.Username)
	}
	s.syncing[a.Username] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.syncing, a.Username); s.mu.Unlock() }()
	if !s.Configured() {
		a.SyncStatus, a.SyncError = "configuration_required", "Instagram veri sağlayıcısı yapılandırılmadı"
		return 0, s.repo.SaveAccount(ctx, *a)
	}
	a.SyncStatus = "syncing"
	a.SyncError = ""
	if total, countErr := s.repo.AccountPostCount(ctx, a.Username); countErr == nil {
		a.TotalPosts = total
	}
	if e := s.repo.SaveAccount(ctx, *a); e != nil {
		return 0, e
	}
	count, e := s.collector.Sync(ctx, a.Username, false, nil)
	// The streaming collector may have enriched the account with profile data.
	if enriched, loadErr := s.repo.Account(ctx, a.Username); loadErr == nil {
		a.DisplayName, a.ProfilePictureURL = enriched.DisplayName, enriched.ProfilePictureURL
	}
	now := syncTime()
	a.LastSyncAt = &now
	if e != nil {
		a.SyncError = e.Error()
		a.SyncStatus = "failed"
		if strings.Contains(e.Error(), "INSTAGRAM_RATE_LIMITED") {
			until := now.Add(30 * time.Minute)
			a.CooldownUntil = &until
			_ = s.repo.SetCooldown(ctx, a.Username, until)
		}
	} else {
		a.SyncError = ""
		a.SyncStatus = "completed"
		if total, countErr := s.repo.AccountPostCount(ctx, a.Username); countErr == nil {
			a.TotalPosts = total
		} else {
			a.TotalPosts += int64(count)
		}
	}
	a.UpdatedAt = now
	a.LastSyncResult = &SyncResult{NewPosts: count, Provider: s.providerName, StartedAt: started, CompletedAt: now, Error: a.SyncError}
	if saveErr := s.repo.SaveAccount(ctx, *a); saveErr != nil && e == nil {
		e = saveErr
	}
	return count, e
}
func (s *Service) StartFullSync(username string) (FullSyncState, error) {
	a, e := s.repo.Account(context.Background(), username)
	if e != nil {
		return FullSyncState{}, e
	}
	s.mu.Lock()
	if s.syncing[a.Username] || s.fullCancels[a.Username] != nil {
		s.mu.Unlock()
		return FullSyncState{}, ErrConflict
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.fullCancels[a.Username] = cancel
	s.syncing[a.Username] = true
	s.mu.Unlock()
	now := syncTime()
	state := FullSyncState{JobID: uuid.NewString(), Username: a.Username, Status: "queued", StartedAt: now, UpdatedAt: now}
	if e = s.repo.SaveFullSyncState(context.Background(), state); e != nil {
		cancel()
		s.mu.Lock()
		delete(s.fullCancels, a.Username)
		delete(s.syncing, a.Username)
		s.mu.Unlock()
		return state, e
	}
	go func() {
		defer func() { s.mu.Lock(); delete(s.fullCancels, a.Username); delete(s.syncing, a.Username); s.mu.Unlock() }()
		state.Status = "running"
		state.UpdatedAt = syncTime()
		_ = s.repo.SaveFullSyncState(context.Background(), state)
		log.Printf("[full-sync] started username=%s unlimited=true", a.Username)
		var count int
		var runErr error
		count, runErr = s.collector.Sync(ctx, a.Username, true, func(ev InstagramScrapeEvent) {
			if ev.Progress != nil {
				state.DiscoveredCount = ev.Progress.Discovered
				state.ProcessedCount = ev.Progress.Processed
				state.SavedCount = count
				state.UpdatedCount = ev.Progress.Updated
				state.SkippedCount = ev.Progress.Skipped
				state.FailedCount = ev.Progress.Failed
				state.ScrollRound = ev.Progress.ScrollRound
				state.UpdatedAt = syncTime()
				_ = s.repo.SaveFullSyncState(context.Background(), state)
			}
			if ev.Post != nil {
				state.ProcessedCount++
				state.LastShortcode = ev.Post.Shortcode
				state.OldestShortcode = ev.Post.Shortcode
				if !ev.Post.PublishedAt.IsZero() {
					state.OldestPublishedAt = ev.Post.PublishedAt.Format(time.RFC3339)
				}
			}
		})
		done := syncTime()
		state.CompletedAt = &done
		state.UpdatedAt = done
		state.SavedCount = count
		if errors.Is(runErr, context.Canceled) {
			state.Status = "cancelled"
			state.CancelRequested = true
		} else if runErr != nil && strings.Contains(runErr.Error(), "INSTAGRAM_RATE_LIMITED") {
			state.Status = "paused_rate_limit"
			state.PauseReason = runErr.Error()
			state.RetryAfter = 1800
		} else if runErr != nil {
			state.Status = "failed"
			state.Error = runErr.Error()
			state.FailedCount++
		} else {
			state.Status = "completed"
			log.Printf("[full-sync] completed discovered=%d", state.DiscoveredCount)
		}
		_ = s.repo.SaveFullSyncState(context.Background(), state)
		if total, x := s.repo.AccountPostCount(context.Background(), a.Username); x == nil {
			a.TotalPosts = total
		}
		a.LastSyncAt = &done
		a.UpdatedAt = done
		a.SyncStatus = state.Status
		a.SyncError = state.Error
		_ = s.repo.SaveAccount(context.Background(), a)
	}()
	return state, nil
}
func (s *Service) FullSyncStatus(ctx context.Context, username string) (FullSyncState, error) {
	return s.repo.FullSyncState(ctx, username)
}
func (s *Service) CancelFullSync(ctx context.Context, username string) (FullSyncState, error) {
	s.mu.Lock()
	cancel := s.fullCancels[cleanUsername(username)]
	s.mu.Unlock()
	if cancel == nil {
		return s.repo.FullSyncState(ctx, username)
	}
	cancel()
	state, e := s.repo.FullSyncState(ctx, username)
	if e == nil {
		state.CancelRequested = true
		state.UpdatedAt = syncTime()
		_ = s.repo.SaveFullSyncState(ctx, state)
	}
	return state, e
}
func (s *Service) SyncActive(ctx context.Context) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	accounts, e := s.repo.Accounts(ctx)
	if e != nil {
		return e
	}
	var failed []string
	for i := range accounts {
		if !accounts[i].Active {
			continue
		}
		if _, x := s.sync(ctx, &accounts[i]); x != nil {
			log.Printf("instagram sync failed username=%s error=%v", accounts[i].Username, x)
			failed = append(failed, accounts[i].Username)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("Instagram senkronizasyonu tamamlanamadı: %s", strings.Join(failed, ", "))
	}
	return nil
}
func (s *Service) List(ctx context.Context, o ListOptions) (ListResult, error) {
	return s.repo.List(ctx, o)
}
func (s *Service) Get(ctx context.Context, id string) (Post, error) { return s.repo.Get(ctx, id) }
func (s *Service) Browser(ctx context.Context, method, path string) (map[string]any, error) {
	p, ok := s.provider.(*InstaloaderHTTPProvider)
	if !ok {
		return nil, ErrNotConfigured
	}
	return p.Browser(ctx, method, path)
}
func SyncTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 10*time.Minute)
}
