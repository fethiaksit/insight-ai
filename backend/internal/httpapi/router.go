package httpapi

import (
	"context"
	"errors"
	"github.com/fethiaksit/social-analytics/internal/documents"
	"github.com/fethiaksit/social-analytics/internal/domain"
	"github.com/fethiaksit/social-analytics/internal/instagram"
	"github.com/fethiaksit/social-analytics/internal/repositories"
	"github.com/fethiaksit/social-analytics/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"strconv"
	"time"
)

func NewRouter(s *services.Service, instagramService *instagram.Service, documentServices ...*documents.Service) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), cors(), errorHandler())
	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := s.Health(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/swagger/openapi.yaml", func(c *gin.Context) { c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec) })
	r.GET("/swagger", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/swagger/openapi.yaml") })
	v := r.Group("/api/v1")
	instagramRoutes(r.Group("/api/instagram"), instagramService)
	if len(documentServices) > 0 && documentServices[0] != nil {
		documents.RegisterRoutes(r.Group("/api/documents"), documentServices[0])
	}
	v.GET("/dashboard", func(c *gin.Context) { data, e := s.Dashboard(c.Request.Context()); respond(c, data, e) })
	v.GET("/accounts", func(c *gin.Context) {
		p, z := pagination(c)
		data, total, e := s.Accounts(c.Request.Context(), p, z)
		respond(c, gin.H{"data": data, "meta": meta(p, z, total)}, e)
	})
	v.POST("/accounts", func(c *gin.Context) {
		var in services.CreateAccountInput
		if e := c.ShouldBindJSON(&in); e != nil {
			c.Error(e)
			return
		}
		data, e := s.CreateAccount(c.Request.Context(), in)
		respondCreated(c, data, e)
	})
	v.PATCH("/accounts/:id", func(c *gin.Context) {
		id, e := uuid.Parse(c.Param("id"))
		if e != nil {
			c.Error(e)
			return
		}
		var in struct {
			Active *bool `json:"active"`
		}
		if e = c.ShouldBindJSON(&in); e != nil {
			c.Error(e)
			return
		}
		data, e := s.UpdateAccount(c.Request.Context(), id, in.Active)
		respond(c, data, e)
	})
	v.DELETE("/accounts/:id", func(c *gin.Context) {
		id, e := uuid.Parse(c.Param("id"))
		if e == nil {
			e = s.DeleteAccount(c.Request.Context(), id)
		}
		if e != nil {
			c.Error(e)
			return
		}
		c.Status(204)
	})
	v.GET("/posts", func(c *gin.Context) {
		p, z := pagination(c)
		min, _ := strconv.ParseFloat(c.Query("minConfidence"), 64)
		data, total, e := s.Posts(c.Request.Context(), p, z, c.Query("search"), c.Query("platform"), c.Query("topic"), min)
		respond(c, gin.H{"data": data, "meta": meta(p, z, total)}, e)
	})
	v.POST("/accounts/:id/posts", func(c *gin.Context) {
		id, e := uuid.Parse(c.Param("id"))
		if e != nil {
			c.Error(e)
			return
		}
		var in services.CreatePostInput
		if e = c.ShouldBindJSON(&in); e != nil {
			c.Error(e)
			return
		}
		data, e := s.CreatePost(c.Request.Context(), id, in)
		respondCreated(c, data, e)
	})
	v.GET("/topics", func(c *gin.Context) { data, e := s.Topics(c.Request.Context()); respond(c, data, e) })
	v.POST("/topics", func(c *gin.Context) {
		var t domain.Topic
		if e := c.ShouldBindJSON(&t); e != nil {
			c.Error(e)
			return
		}
		e := s.CreateTopic(c.Request.Context(), &t)
		respondCreated(c, t, e)
	})
	return r
}
func instagramRoutes(r *gin.RouterGroup, s *instagram.Service) {
	r.POST("/browser/open", func(c *gin.Context) {
		data, e := s.Browser(c.Request.Context(), http.MethodPost, "/browser/open")
		respond(c, data, e)
	})
	r.GET("/browser/status", func(c *gin.Context) {
		data, e := s.Browser(c.Request.Context(), http.MethodGet, "/browser/status")
		respond(c, data, e)
	})
	r.GET("/status", func(c *gin.Context) { data, e := s.Status(c.Request.Context()); respond(c, data, e) })
	r.GET("/accounts", func(c *gin.Context) { data, e := s.Accounts(c.Request.Context()); respond(c, data, e) })
	r.POST("/accounts", func(c *gin.Context) {
		var input struct {
			Username string `json:"username"`
			Profile  string `json:"profile"`
		}
		if e := c.ShouldBindJSON(&input); e != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Geçerli bir JSON body ve username alanı gerekli"}})
			return
		}
		value := input.Profile
		if value == "" {
			value = input.Username
		}
		data, e := s.AddAccount(c.Request.Context(), value)
		if e != nil {
			c.Error(e)
			return
		}
		if data.SyncStatus == "pending" {
			s.StartSync(data.Username)
		}
		c.JSON(http.StatusCreated, gin.H{"success": true, "account": gin.H{
			"id": data.ID, "username": data.Username, "profile_url": data.ProfileURL,
			"status": data.SyncStatus, "sync_status": data.SyncStatus, "sync_error": data.SyncError,
			"last_sync_at": data.LastSyncAt, "total_posts": data.TotalPosts, "active": data.Active,
		}})
	})
	r.PATCH("/accounts/:id", func(c *gin.Context) {
		var input struct {
			Username *string `json:"username"`
			Active   *bool   `json:"active"`
		}
		if e := c.ShouldBindJSON(&input); e != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Geçerli bir JSON body gerekli"}})
			return
		}
		if input.Username == nil && input.Active == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "username veya active alanlarından biri gerekli"}})
			return
		}
		data, e := s.UpdateAccount(c.Request.Context(), c.Param("id"), input.Username, input.Active)
		respond(c, data, e)
	})
	r.DELETE("/accounts/:id", func(c *gin.Context) {
		if e := s.DeleteAccount(c.Request.Context(), c.Param("id")); e != nil {
			c.Error(e)
			return
		}
		c.Status(http.StatusNoContent)
	})
	r.POST("/accounts/:id/sync", func(c *gin.Context) {
		// Browser scraping uzun sürebilir. İşi request context'ine bağlamak,
		// istemci timeout olduğunda hesabı kalıcı olarak "syncing" bırakıyordu.
		s.StartSync(c.Param("id"))
		c.JSON(http.StatusAccepted, gin.H{"status": "syncing"})
	})
	r.POST("/accounts/:id/full-sync", func(c *gin.Context) {
		state, e := s.StartFullSync(c.Param("id"))
		if errors.Is(e, instagram.ErrConflict) {
			c.JSON(http.StatusConflict, gin.H{"code": "SYNC_ALREADY_RUNNING", "message": "Bu hesabın geçmiş taraması zaten devam ediyor."})
			return
		}
		if e != nil {
			c.Error(e)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"job_id": state.JobID, "status": "queued"})
	})
	r.GET("/accounts/:id/sync-status", func(c *gin.Context) {
		data, e := s.FullSyncStatus(c.Request.Context(), c.Param("id"))
		respond(c, data, e)
	})
	r.POST("/accounts/:id/sync-cancel", func(c *gin.Context) {
		data, e := s.CancelFullSync(c.Request.Context(), c.Param("id"))
		respond(c, data, e)
	})
	// Backwards-compatible manual sync endpoint used by earlier clients.
	r.POST("/sync", func(c *gin.Context) {
		var input struct {
			Username string `json:"username"`
		}
		if e := c.ShouldBindJSON(&input); e != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Geçerli bir JSON body ve username alanı gerekli"}})
			return
		}
		ctx, cancel := instagram.SyncTimeout(c.Request.Context())
		defer cancel()
		count, e := s.Sync(ctx, input.Username)
		if e != nil {
			c.Error(e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"synced": count})
	})
	r.GET("/posts", func(c *gin.Context) {
		page, limit := instagramPagination(c)
		start, e := parseDate(c.Query("start_date"))
		if e != nil {
			c.Error(e)
			return
		}
		end, e := parseDate(c.Query("end_date"))
		if e != nil {
			c.Error(e)
			return
		}
		search := c.Query("keyword")
		if search == "" {
			search = c.Query("keywords")
		}
		if search == "" {
			search = c.Query("search")
		}
		username := c.Query("account_id")
		if username == "" {
			username = c.Query("username")
		}
		data, e := s.List(c.Request.Context(), instagram.ListOptions{Page: page, Limit: limit, Username: username, Search: search, MediaType: c.Query("media_type"), Match: c.DefaultQuery("match", "any"), StartDate: start, EndDate: end, Sort: c.DefaultQuery("sort", "newest")})
		respond(c, data, e)
	})
	r.GET("/posts/:id", func(c *gin.Context) { data, e := s.Get(c.Request.Context(), c.Param("id")); respond(c, data, e) })
}
func instagramPagination(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size := c.Query("page_size")
	if size == "" {
		size = c.DefaultQuery("limit", "50")
	}
	l, _ := strconv.Atoi(size)
	if p < 1 {
		p = 1
	}
	if l < 1 {
		l = 50
	}
	if l > 100 {
		l = 100
	}
	return p, l
}
func parseDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if v, e := time.Parse(layout, value); e == nil {
			return &v, nil
		}
	}
	return nil, errors.New("tarih YYYY-MM-DD veya RFC3339 biçiminde olmalı")
}
func pagination(c *gin.Context) (int, int) {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	z, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if p < 1 {
		p = 1
	}
	if z < 1 {
		z = 20
	}
	if z > 100 {
		z = 100
	}
	return p, z
}
func meta(p, z int, total int64) gin.H { return gin.H{"page": p, "pageSize": z, "total": total} }
func respond(c *gin.Context, v any, e error) {
	if e != nil {
		c.Error(e)
		return
	}
	c.JSON(200, v)
}
func respondCreated(c *gin.Context, v any, e error) {
	if e != nil {
		c.Error(e)
		return
	}
	c.JSON(201, v)
}
func errorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 {
			return
		}
		e := c.Errors.Last().Err
		status := http.StatusInternalServerError
		if errors.Is(e, repositories.ErrNotFound) {
			status = 404
		} else if errors.Is(e, instagram.ErrNotFound) {
			status = 404
		} else if errors.Is(e, instagram.ErrNotConfigured) {
			status = http.StatusServiceUnavailable
		} else if errors.Is(e, instagram.ErrConflict) {
			status = http.StatusConflict
		} else if errors.Is(e, instagram.ErrInvalidInput) {
			status = http.StatusBadRequest
		} else if errors.Is(e, repositories.ErrConflict) {
			status = http.StatusConflict
		} else if _, ok := e.(*strconv.NumError); ok {
			status = 400
		}
		c.JSON(status, gin.H{"error": gin.H{"message": e.Error()}})
	}
}
func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Request.Method == "OPTIONS" {
			c.Status(204)
			c.Abort()
			return
		}
		c.Next()
	}
}
