package instagram

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

type Service struct {
	provider     InstagramProvider
	providerName string
	repo         *Repository
	collector    *Collector
}

func NewService(provider InstagramProvider, providerName string, repo *Repository) *Service {
	return &Service{provider: provider, providerName: providerName, repo: repo, collector: NewCollector(provider, repo)}
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
	a := Account{ID: username, Username: username, DisplayName: "@" + username, ProfileURL: canonicalProfileURL(username), Active: true, CreatedAt: syncTime(), SyncStatus: "syncing"}
	if !s.Configured() {
		a.SyncStatus, a.SyncError = "configuration_required", "Instagram veri sağlayıcısı yapılandırılmadı"
	}
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
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
	if !s.Configured() {
		a.SyncStatus, a.SyncError = "configuration_required", "Instagram veri sağlayıcısı yapılandırılmadı"
		return 0, s.repo.SaveAccount(ctx, *a)
	}
	a.SyncStatus = "syncing"
	a.SyncError = ""
	if e := s.repo.SaveAccount(ctx, *a); e != nil {
		return 0, e
	}
	profile, e := s.provider.GetProfile(ctx, a.Username)
	if e == nil && profile != nil {
		a.DisplayName, a.ProfilePictureURL = profile.Name, profile.ProfilePictureURL
	}
	var count int
	if e == nil {
		count, e = s.collector.Sync(ctx, a.Username)
	}
	now := syncTime()
	a.LastSyncAt = &now
	if e != nil {
		a.SyncError = e.Error()
		a.SyncStatus = "error"
	} else {
		a.SyncError = ""
		a.SyncStatus = "success"
		a.TotalPosts += int64(count)
	}
	if saveErr := s.repo.SaveAccount(ctx, *a); saveErr != nil && e == nil {
		e = saveErr
	}
	return count, e
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
func SyncTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 10*time.Minute)
}
