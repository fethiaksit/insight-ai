package httpapi

import (
	"github.com/fethiaksit/social-analytics/internal/instagram"
	"github.com/go-redis/redis/v8"
	"testing"
)

func TestInstagramAccountRoutesAreRegistered(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	router := NewRouter(nil, instagram.NewService(nil, "", instagram.NewRepository(client)))
	want := map[string]bool{
		"GET /api/instagram/accounts":           false,
		"POST /api/instagram/accounts":          false,
		"PATCH /api/instagram/accounts/:id":     false,
		"DELETE /api/instagram/accounts/:id":    false,
		"POST /api/instagram/accounts/:id/sync": false,
		"POST /api/instagram/sync":              false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("route not registered: %s", route)
		}
	}
}
