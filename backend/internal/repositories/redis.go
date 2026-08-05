package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fethiaksit/social-analytics/internal/domain"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("resource already exists")
)

// RedisRepository stores each business entity as a JSON value and maintains small set indexes for lookup.
type RedisRepository struct{ client *redis.Client }

func NewRedisRepository(client *redis.Client) *RedisRepository {
	return &RedisRepository{client: client}
}
func (r *RedisRepository) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

func (r *RedisRepository) CreateAccount(ctx context.Context, a *domain.Account) error {
	a.ID = uuid.New()
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	if err := r.put(ctx, accountKey(a.ID), a); err != nil {
		return err
	}
	return r.client.SAdd(ctx, "accounts", a.ID.String()).Err()
}
func (r *RedisRepository) Accounts(ctx context.Context) ([]domain.Account, error) {
	ids, e := r.client.SMembers(ctx, "accounts").Result()
	if e != nil {
		return nil, e
	}
	out := make([]domain.Account, 0, len(ids))
	for _, id := range ids {
		a, e := r.account(ctx, id)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (r *RedisRepository) Account(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	return r.account(ctx, id.String())
}
func (r *RedisRepository) SaveAccount(ctx context.Context, a *domain.Account) error {
	a.UpdatedAt = time.Now().UTC()
	return r.put(ctx, accountKey(a.ID), a)
}
func (r *RedisRepository) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	if _, e := r.Account(ctx, id); e != nil {
		return e
	}
	posts, e := r.client.SMembers(ctx, accountPostsKey(id)).Result()
	if e != nil {
		return e
	}
	pipe := r.client.TxPipeline()
	for _, postID := range posts {
		if p, e := r.postByString(ctx, postID); e == nil {
			pipe.Del(ctx, externalPostKey(p.ExternalID))
		}
		pipe.Del(ctx, postKey(postID), analysisKey(postID))
		pipe.SRem(ctx, "posts", postID)
		pipe.SRem(ctx, "analyses", postID)
	}
	pipe.Del(ctx, accountKey(id), accountPostsKey(id))
	pipe.SRem(ctx, "accounts", id.String())
	_, e = pipe.Exec(ctx)
	return e
}

func (r *RedisRepository) CreatePost(ctx context.Context, p *domain.Post) error {
	if _, e := r.Account(ctx, p.AccountID); e != nil {
		return e
	}
	p.ID = uuid.New()
	p.CreatedAt = time.Now().UTC()
	ok, e := r.client.SetNX(ctx, externalPostKey(p.ExternalID), p.ID.String(), 0).Result()
	if e != nil {
		return e
	}
	if !ok {
		return ErrConflict
	}
	if e = r.put(ctx, postKey(p.ID), p); e != nil {
		r.client.Del(ctx, externalPostKey(p.ExternalID))
		return e
	}
	pipe := r.client.TxPipeline()
	pipe.SAdd(ctx, "posts", p.ID.String())
	pipe.SAdd(ctx, accountPostsKey(p.AccountID), p.ID.String())
	_, e = pipe.Exec(ctx)
	return e
}
func (r *RedisRepository) Post(ctx context.Context, id uuid.UUID) (*domain.Post, error) {
	var p domain.Post
	if e := r.get(ctx, postKey(id), &p); e != nil {
		return nil, e
	}
	return &p, nil
}
func (r *RedisRepository) Posts(ctx context.Context) ([]domain.Post, error) {
	ids, e := r.client.SMembers(ctx, "posts").Result()
	if e != nil {
		return nil, e
	}
	out := make([]domain.Post, 0, len(ids))
	for _, id := range ids {
		p, e := r.postByString(ctx, id)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt.After(out[j].PublishedAt) })
	return out, nil
}
func (r *RedisRepository) SaveAnalysis(ctx context.Context, a *domain.AIAnalysis) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if e := r.put(ctx, analysisKey(a.PostID), a); e != nil {
		return e
	}
	return r.client.SAdd(ctx, "analyses", a.PostID.String()).Err()
}
func (r *RedisRepository) Analysis(ctx context.Context, postID uuid.UUID) (*domain.AIAnalysis, error) {
	var a domain.AIAnalysis
	if e := r.get(ctx, analysisKey(postID), &a); e != nil {
		return nil, e
	}
	return &a, nil
}

func (r *RedisRepository) Topics(ctx context.Context) ([]domain.Topic, error) {
	ids, e := r.client.SMembers(ctx, "topics").Result()
	if e != nil {
		return nil, e
	}
	out := make([]domain.Topic, 0, len(ids))
	for _, id := range ids {
		var t domain.Topic
		e = r.get(ctx, topicKey(id), &t)
		if errors.Is(e, ErrNotFound) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (r *RedisRepository) CreateTopic(ctx context.Context, t *domain.Topic) error {
	topics, e := r.Topics(ctx)
	if e != nil {
		return e
	}
	for _, existing := range topics {
		if strings.EqualFold(existing.Name, t.Name) {
			return ErrConflict
		}
	}
	t.ID = uuid.New()
	t.CreatedAt = time.Now().UTC()
	if e = r.put(ctx, topicKey(t.ID.String()), t); e != nil {
		return e
	}
	return r.client.SAdd(ctx, "topics", t.ID.String()).Err()
}
func (r *RedisRepository) Settings(ctx context.Context) (*domain.Settings, error) {
	var s domain.Settings
	e := r.get(ctx, "settings:default", &s)
	if errors.Is(e, ErrNotFound) {
		return &domain.Settings{ID: "default"}, nil
	}
	return &s, e
}
func (r *RedisRepository) SaveSettings(ctx context.Context, s *domain.Settings) error {
	s.ID = "default"
	s.UpdatedAt = time.Now().UTC()
	return r.put(ctx, "settings:default", s)
}

func (r *RedisRepository) account(ctx context.Context, id string) (*domain.Account, error) {
	var a domain.Account
	if e := r.get(ctx, accountKeyFromString(id), &a); e != nil {
		return nil, e
	}
	return &a, nil
}
func (r *RedisRepository) postByString(ctx context.Context, id string) (*domain.Post, error) {
	var p domain.Post
	if e := r.get(ctx, postKey(id), &p); e != nil {
		return nil, e
	}
	return &p, nil
}
func (r *RedisRepository) put(ctx context.Context, key string, v any) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return r.client.Set(ctx, key, raw, 0).Err()
}
func (r *RedisRepository) get(ctx context.Context, key string, v any) error {
	raw, e := r.client.Get(ctx, key).Bytes()
	if errors.Is(e, redis.Nil) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if e = json.Unmarshal(raw, v); e != nil {
		return fmt.Errorf("decode %s: %w", key, e)
	}
	return nil
}
func accountKey(id uuid.UUID) string        { return accountKeyFromString(id.String()) }
func accountKeyFromString(id string) string { return "account:" + id }
func postKey(id any) string                 { return "post:" + fmt.Sprint(id) }
func analysisKey(id any) string             { return "analysis:" + fmt.Sprint(id) }
func topicKey(id string) string             { return "topic:" + id }
func accountPostsKey(id uuid.UUID) string   { return "account_posts:" + id.String() }
func externalPostKey(id string) string      { return "post_external:" + id }
