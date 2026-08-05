package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fethiaksit/social-analytics/internal/config"
	"github.com/fethiaksit/social-analytics/internal/httpapi"
	"github.com/fethiaksit/social-analytics/internal/instagram"
	"github.com/fethiaksit/social-analytics/internal/repositories"
	"github.com/fethiaksit/social-analytics/internal/scheduler"
	"github.com/fethiaksit/social-analytics/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

type Application struct {
	Router    *gin.Engine
	Server    *http.Server
	Scheduler *scheduler.Scheduler
}

func New() (*Application, error) {
	cfg, e := config.Load()
	if e != nil {
		return nil, fmt.Errorf("config: %w", e)
	}
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	options, e := redis.ParseURL(cfg.RedisURL)
	if e != nil {
		return nil, fmt.Errorf("redis URL: %w", e)
	}
	client := redis.NewClient(options)
	ctx := context.Background()
	for attempt := 1; attempt <= cfg.ConnectRetries; attempt++ {
		e = client.Ping(ctx).Err()
		if e == nil {
			break
		}
		if attempt < cfg.ConnectRetries {
			time.Sleep(cfg.ConnectRetryDelay)
		}
	}
	if e != nil {
		log.Printf("Redis bağlantısı kurulamadı; API sınırlı modda başlatılıyor: %v", e)
	}
	repo := repositories.NewRedisRepository(client)
	ai := services.NewAIService(cfg.OpenAIKey, cfg.OpenAIModel, cfg.EmbeddingModel)
	svc := services.New(repo, ai)
	var instagramProvider instagram.InstagramProvider = instagram.NewInstaloaderHTTPProvider(cfg.InstagramScraperURL, cfg.InstagramScraperTimeout)
	instagramService := instagram.NewService(instagramProvider, cfg.InstagramProvider, instagram.NewRepository(client))
	router := httpapi.NewRouter(svc, instagramService)
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/instagram/") {
			log.Printf("route registered: %s %s", route.Method, route.Path)
		}
	}
	server := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 11 * time.Minute, IdleTimeout: 60 * time.Second}
	return &Application{Router: router, Server: server, Scheduler: scheduler.New(svc, cfg.ScanCron, instagramService, cfg.InstagramSyncCron)}, nil
}
