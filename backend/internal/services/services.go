package services

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/fethiaksit/social-analytics/internal/domain"
	"github.com/fethiaksit/social-analytics/internal/repositories"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Service struct {
	repo *repositories.RedisRepository
	ai   *AIService
}

func New(repo *repositories.RedisRepository, ai *AIService) *Service {
	return &Service{repo: repo, ai: ai}
}

type CreateAccountInput struct {
	Platform    domain.Platform `json:"platform" binding:"required,oneof=x instagram youtube rss"`
	Username    string          `json:"username" binding:"required,min=1,max=255"`
	ProfileName string          `json:"profileName" binding:"required,max=255"`
}

func (s *Service) Health(ctx context.Context) error { return s.repo.Ping(ctx) }
func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (*domain.Account, error) {
	a := &domain.Account{Platform: in.Platform, Username: strings.TrimSpace(in.Username), ProfileName: strings.TrimSpace(in.ProfileName), Active: true}
	return a, s.repo.CreateAccount(ctx, a)
}
func (s *Service) Accounts(ctx context.Context, page, size int) ([]domain.Account, int64, error) {
	all, e := s.repo.Accounts(ctx)
	if e != nil {
		return nil, 0, e
	}
	total := int64(len(all))
	return paginate(all, page, size), total, nil
}
func (s *Service) UpdateAccount(ctx context.Context, id uuid.UUID, active *bool) (*domain.Account, error) {
	a, e := s.repo.Account(ctx, id)
	if e != nil {
		return nil, e
	}
	if active != nil {
		a.Active = *active
	}
	return a, s.repo.SaveAccount(ctx, a)
}
func (s *Service) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteAccount(ctx, id)
}

type CreatePostInput struct {
	ExternalID  string    `json:"externalId" binding:"required,max=512"`
	Content     string    `json:"content" binding:"required,min=1"`
	URL         string    `json:"url"`
	PostType    string    `json:"postType"`
	PublishedAt time.Time `json:"publishedAt" binding:"required"`
}

func (s *Service) CreatePost(ctx context.Context, accountID uuid.UUID, in CreatePostInput) (*domain.Post, error) {
	p := &domain.Post{AccountID: accountID, ExternalID: in.ExternalID, Content: in.Content, URL: in.URL, PostType: defaultString(in.PostType, "post"), PublishedAt: in.PublishedAt}
	if e := s.repo.CreatePost(ctx, p); e != nil {
		return nil, e
	}
	if _, e := s.AnalyzePost(ctx, p.ID); e != nil {
		return p, e
	}
	return p, nil
}
func (s *Service) Posts(ctx context.Context, page, size int, search, platform, topic string, minConfidence float64) ([]domain.Post, int64, error) {
	all, e := s.repo.Posts(ctx)
	if e != nil {
		return nil, 0, e
	}
	filtered := make([]domain.Post, 0, len(all))
	for _, p := range all {
		a, e := s.repo.Account(ctx, p.AccountID)
		if e != nil {
			continue
		}
		if platform != "" && string(a.Platform) != platform {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(p.Content), strings.ToLower(search)) {
			continue
		}
		analysis, e := s.repo.Analysis(ctx, p.ID)
		if topic != "" && (e != nil || analysis.MainTopic != topic) {
			continue
		}
		if minConfidence > 0 && (e != nil || analysis.Confidence < minConfidence) {
			continue
		}
		p.Account = *a
		if e == nil {
			p.Analysis = analysis
		}
		filtered = append(filtered, p)
	}
	total := int64(len(filtered))
	return paginate(filtered, page, size), total, nil
}
func (s *Service) AnalyzePost(ctx context.Context, id uuid.UUID) (*domain.AIAnalysis, error) {
	p, e := s.repo.Post(ctx, id)
	if e != nil {
		return nil, e
	}
	result, e := s.ai.Analyze(ctx, p.Content)
	if e != nil {
		return nil, e
	}
	topics, e := s.repo.Topics(ctx)
	if e != nil {
		return nil, e
	}
	relevant := false
	for _, topic := range topics {
		if !topic.Active {
			continue
		}
		embedding, e := s.ai.Embed(ctx, topic.Name+" "+topic.Description)
		if e != nil {
			return nil, e
		}
		if cosine(result.Embedding, embedding) >= .60 {
			relevant = true
			break
		}
	}
	result.PostID = p.ID
	result.IsRelevant = relevant
	result.AnalyzedAt = time.Now().UTC()
	return result, s.repo.SaveAnalysis(ctx, result)
}
func (s *Service) Dashboard(ctx context.Context) (gin.H, error) {
	accounts, e := s.repo.Accounts(ctx)
	if e != nil {
		return nil, e
	}
	posts, e := s.repo.Posts(ctx)
	if e != nil {
		return nil, e
	}
	var today, matches int64
	var last *time.Time
	for _, a := range accounts {
		if a.LastScannedAt != nil && (last == nil || a.LastScannedAt.After(*last)) {
			last = a.LastScannedAt
		}
	}
	for _, p := range posts {
		if p.CreatedAt.After(time.Now().UTC().Add(-24 * time.Hour)) {
			today++
		}
		if analysis, e := s.repo.Analysis(ctx, p.ID); e == nil && analysis.IsRelevant {
			matches++
		}
	}
	return gin.H{"trackedAccounts": len(accounts), "analyzedPosts": len(posts), "todayAnalyzed": today, "aiMatches": matches, "lastScanAt": last, "status": "healthy"}, nil
}
func (s *Service) Topics(ctx context.Context) ([]domain.Topic, error) { return s.repo.Topics(ctx) }
func (s *Service) CreateTopic(ctx context.Context, t *domain.Topic) error {
	return s.repo.CreateTopic(ctx, t)
}
func (s *Service) ScanActiveAccounts(ctx context.Context) error {
	accounts, e := s.repo.Accounts(ctx)
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	for i := range accounts {
		if accounts[i].Active {
			accounts[i].LastScannedAt = &now
			if e = s.repo.SaveAccount(ctx, &accounts[i]); e != nil {
				return e
			}
		}
	}
	return nil
}
func defaultString(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, aa, bb float64
	for i := range a {
		dot += float64(a[i] * b[i])
		aa += float64(a[i] * a[i])
		bb += float64(b[i] * b[i])
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return dot / (math.Sqrt(aa) * math.Sqrt(bb))
}
func paginate[T any](items []T, page, size int) []T {
	start := (page - 1) * size
	if start >= len(items) {
		return []T{}
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
