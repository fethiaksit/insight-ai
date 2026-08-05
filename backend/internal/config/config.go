package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv, RedisURL, OpenAIKey, OpenAIModel, EmbeddingModel, ScanCron, InstagramSyncCron, Port string
	InstagramProvider, InstagramProviderAPIKey, InstagramProviderBaseURL                        string
	InstagramProviderTimeout, InstagramProviderMinInterval                                      time.Duration
	ConnectRetries                                                                              int
	ConnectRetryDelay                                                                           time.Duration
}

func Load() (Config, error) {
	retries, e := strconv.Atoi(value("REDIS_CONNECT_RETRIES", "30"))
	if e != nil || retries < 1 {
		return Config{}, fmt.Errorf("REDIS_CONNECT_RETRIES must be a positive integer")
	}
	delay, e := time.ParseDuration(value("REDIS_CONNECT_RETRY_DELAY", "2s"))
	if e != nil || delay <= 0 {
		return Config{}, fmt.Errorf("REDIS_CONNECT_RETRY_DELAY must be a positive duration")
	}
	timeout, e := time.ParseDuration(value("INSTAGRAM_PROVIDER_TIMEOUT", "20s"))
	if e != nil || timeout <= 0 {
		return Config{}, fmt.Errorf("INSTAGRAM_PROVIDER_TIMEOUT must be a positive duration")
	}
	interval, e := time.ParseDuration(value("INSTAGRAM_PROVIDER_MIN_INTERVAL", "250ms"))
	if e != nil || interval < 0 {
		return Config{}, fmt.Errorf("INSTAGRAM_PROVIDER_MIN_INTERVAL must be a non-negative duration")
	}
	return Config{AppEnv: value("APP_ENV", "development"), RedisURL: redisURL(), OpenAIKey: os.Getenv("OPENAI_API_KEY"), OpenAIModel: value("OPENAI_MODEL", "gpt-4.1-mini"), EmbeddingModel: value("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"), ScanCron: value("SCAN_CRON", "*/15 * * * *"), InstagramSyncCron: value("INSTAGRAM_SYNC_CRON", "*/30 * * * *"), Port: value("BACKEND_PORT", "8080"), InstagramProvider: strings.ToLower(strings.TrimSpace(os.Getenv("INSTAGRAM_PROVIDER"))), InstagramProviderAPIKey: os.Getenv("INSTAGRAM_PROVIDER_API_KEY"), InstagramProviderBaseURL: os.Getenv("INSTAGRAM_PROVIDER_BASE_URL"), InstagramProviderTimeout: timeout, InstagramProviderMinInterval: interval, ConnectRetries: retries, ConnectRetryDelay: delay}, nil
}
func redisURL() string {
	if v := os.Getenv("REDIS_URL"); v != "" {
		return v
	}
	return "redis://" + value("REDIS_HOST", "localhost") + ":" + value("REDIS_PORT", "6379") + "/0"
}
func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
