package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-redis/redis/v8"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

type Repository struct{ client *redis.Client }

func NewRepository(c *redis.Client) *Repository { return &Repository{client: c} }

func (r *Repository) AddAccount(ctx context.Context, a Account) error {
	if a.ID == "" {
		a.ID = a.Username
	}
	if a.ProfileURL == "" {
		a.ProfileURL = canonicalProfileURL(a.Username)
	}
	added, err := r.client.SAdd(ctx, "instagram:accounts", a.Username).Result()
	if err != nil {
		return err
	}
	if added == 0 {
		return ErrConflict
	}
	raw, _ := json.Marshal(a)
	if err = r.client.Set(ctx, "instagram:account:"+a.Username, raw, 0).Err(); err != nil {
		_ = r.client.SRem(ctx, "instagram:accounts", a.Username).Err()
	}
	return err
}
func (r *Repository) SaveAccount(ctx context.Context, a Account) error {
	raw, e := json.Marshal(a)
	if e != nil {
		return e
	}
	return r.client.Set(ctx, "instagram:account:"+a.Username, raw, 0).Err()
}
func (r *Repository) Account(ctx context.Context, username string) (Account, error) {
	var a Account
	raw, e := r.client.Get(ctx, "instagram:account:"+cleanUsername(username)).Bytes()
	if errors.Is(e, redis.Nil) {
		return a, ErrNotFound
	}
	if e != nil {
		return a, e
	}
	e = json.Unmarshal(raw, &a)
	if a.ID == "" {
		a.ID = a.Username
	}
	if a.ProfileURL == "" {
		a.ProfileURL = canonicalProfileURL(a.Username)
	}
	return a, e
}
func (r *Repository) Accounts(ctx context.Context) ([]Account, error) {
	ids, e := r.client.SMembers(ctx, "instagram:accounts").Result()
	if e != nil {
		return nil, e
	}
	out := make([]Account, 0, len(ids))
	for _, id := range ids {
		a, x := r.Account(ctx, id)
		if x == nil {
			out = append(out, a)
		} else if !errors.Is(x, ErrNotFound) {
			return nil, x
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

// UpdateAccount safely moves the account index when its username changes.
// Global posts are retained and their username metadata is updated.
func (r *Repository) UpdateAccount(ctx context.Context, id string, a Account) error {
	id, a.Username = cleanUsername(id), cleanUsername(a.Username)
	if id == a.Username {
		return r.SaveAccount(ctx, a)
	}
	if _, e := r.Account(ctx, a.Username); e == nil {
		return ErrConflict
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	postIDs, e := r.client.SMembers(ctx, "instagram:account:posts:"+id).Result()
	if e != nil {
		return e
	}
	raw, e := json.Marshal(a)
	if e != nil {
		return e
	}
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, "instagram:account:"+a.Username, raw, 0)
	pipe.SAdd(ctx, "instagram:accounts", a.Username)
	pipe.SRem(ctx, "instagram:accounts", id)
	pipe.Del(ctx, "instagram:account:"+id)
	for _, postID := range postIDs {
		postRaw, getErr := r.client.Get(ctx, "instagram:post:"+postID).Bytes()
		if getErr != nil {
			if errors.Is(getErr, redis.Nil) {
				continue
			}
			return getErr
		}
		var post Post
		if e = json.Unmarshal(postRaw, &post); e != nil {
			return e
		}
		post.Username = a.Username
		postRaw, e = json.Marshal(post)
		if e != nil {
			return e
		}
		pipe.Set(ctx, "instagram:post:"+postID, postRaw, 0)
		pipe.SAdd(ctx, "instagram:account:posts:"+a.Username, postID)
	}
	pipe.Del(ctx, "instagram:account:posts:"+id)
	_, e = pipe.Exec(ctx)
	return e
}

// DeleteAccount removes tracking metadata only. Collected posts remain in the
// global archive so deleting an account cannot cause accidental data loss.
func (r *Repository) DeleteAccount(ctx context.Context, id string) error {
	id = cleanUsername(id)
	if _, e := r.Account(ctx, id); e != nil {
		return e
	}
	pipe := r.client.TxPipeline()
	pipe.SRem(ctx, "instagram:accounts", id)
	pipe.Del(ctx, "instagram:account:"+id)
	pipe.Del(ctx, "instagram:account:posts:"+id)
	_, e := pipe.Exec(ctx)
	return e
}

// SavePosts uses Redis sets as uniqueness indexes. External id is preferred;
// permalink is also indexed so a provider id change cannot duplicate a post.
func (r *Repository) SavePosts(ctx context.Context, posts []Post) (int, error) {
	created := 0
	for _, p := range posts {
		p.Username = cleanUsername(p.Username)
		p.Platform = "instagram"
		if p.CollectedAt.IsZero() {
			p.CollectedAt = time.Now().UTC()
		}
		key := p.ExternalID
		if key == "" {
			key = p.Permalink
		}
		if key == "" {
			continue
		}
		unique := "instagram:unique:id:" + p.ExternalID
		if p.ExternalID == "" {
			unique = "instagram:unique:url:" + p.Permalink
		}
		ok, e := r.client.SetNX(ctx, unique, key, 0).Result()
		if e != nil {
			return created, e
		}
		if !ok {
			continue
		}
		if p.ExternalID != "" && p.Permalink != "" {
			urlOK, e := r.client.SetNX(ctx, "instagram:unique:url:"+p.Permalink, key, 0).Result()
			if e != nil {
				return created, e
			}
			if !urlOK {
				_ = r.client.Del(ctx, unique).Err()
				continue
			}
		}
		raw, e := json.Marshal(p)
		if e != nil {
			return created, e
		}
		pipe := r.client.TxPipeline()
		pipe.Set(ctx, "instagram:post:"+key, raw, 0)
		pipe.SAdd(ctx, "instagram:posts", key)
		score := float64(p.PublishedAt.Unix())
		pipe.ZAdd(ctx, "instagram:account:"+p.Username+":posts", &redis.Z{Score: score, Member: key})
		if _, e = pipe.Exec(ctx); e != nil {
			return created, e
		}
		created++
	}
	return created, nil
}
func (r *Repository) KnownShortcodes(ctx context.Context, username string, limit int64) ([]string, error) {
	ids, e := r.client.ZRevRange(ctx, "instagram:account:"+cleanUsername(username)+":posts", 0, limit-1).Result()
	if e != nil {
		return nil, e
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		p, x := r.Get(ctx, id)
		if x == nil && p.Shortcode != "" {
			out = append(out, p.Shortcode)
		}
	}
	return out, nil
}
func (r *Repository) TotalPosts(ctx context.Context) (int64, error) {
	return r.client.SCard(ctx, "instagram:posts").Result()
}
func (r *Repository) List(ctx context.Context, o ListOptions) (ListResult, error) {
	ids, e := r.client.SMembers(ctx, "instagram:posts").Result()
	if e != nil {
		return ListResult{}, e
	}
	all := make([]Post, 0, len(ids))
	for _, id := range ids {
		raw, x := r.client.Get(ctx, "instagram:post:"+id).Bytes()
		if errors.Is(x, redis.Nil) {
			continue
		}
		if x != nil {
			return ListResult{}, x
		}
		var p Post
		if x = json.Unmarshal(raw, &p); x != nil {
			return ListResult{}, x
		}
		if matches(p, o) {
			all = append(all, p)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if o.Sort == "oldest" || o.Sort == "asc" {
			return all[i].PublishedAt.Before(all[j].PublishedAt)
		}
		return all[i].PublishedAt.After(all[j].PublishedAt)
	})
	total := len(all)
	start := (o.Page - 1) * o.Limit
	if start > total {
		start = total
	}
	end := start + o.Limit
	if end > total {
		end = total
	}
	return ListResult{Data: all[start:end], Meta: Meta{Page: o.Page, Limit: o.Limit, Total: int64(total)}}, nil
}
func (r *Repository) Get(ctx context.Context, id string) (Post, error) {
	raw, e := r.client.Get(ctx, "instagram:post:"+id).Bytes()
	if errors.Is(e, redis.Nil) {
		return Post{}, ErrNotFound
	}
	var p Post
	if e == nil {
		e = json.Unmarshal(raw, &p)
	}
	return p, e
}
func matches(p Post, o ListOptions) bool {
	if o.Username != "" && cleanUsername(p.Username) != cleanUsername(o.Username) {
		return false
	}
	if o.MediaType != "" && !strings.EqualFold(p.MediaType, o.MediaType) {
		return false
	}
	if o.StartDate != nil && p.PublishedAt.Before(*o.StartDate) {
		return false
	}
	if o.EndDate != nil && p.PublishedAt.After(o.EndDate.Add(24*time.Hour)) {
		return false
	}
	words := keywords(o.Search)
	if len(words) == 0 {
		return true
	}
	body := turkishFold(p.Caption)
	hits := 0
	for _, w := range words {
		if strings.Contains(body, w) {
			hits++
		}
	}
	return (o.Match == "all" && hits == len(words)) || (o.Match != "all" && hits > 0)
}
func keywords(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, v := range fields {
		if v = strings.TrimSpace(turkishFold(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func turkishFold(s string) string {
	s = norm.NFD.String(s)
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, s)
	return cases.Lower(language.Turkish).String(s)
}
