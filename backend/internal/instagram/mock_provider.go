package instagram

import (
	"context"
	"time"
)

// MockInstagramProvider exists only for explicit development/UI testing.
type MockInstagramProvider struct{}

func NewMockInstagramProvider() *MockInstagramProvider { return &MockInstagramProvider{} }
func (p *MockInstagramProvider) GetProfile(_ context.Context, username string) (*Profile, error) {
	username = cleanUsername(username)
	return &Profile{Username: username, Name: "Demo @" + username}, nil
}
func (p *MockInstagramProvider) GetPosts(_ context.Context, username, cursor string) (*PostPage, error) {
	if cursor != "" {
		return &PostPage{Posts: []Post{}}, nil
	}
	username = cleanUsername(username)
	now := time.Now().UTC()
	return &PostPage{Posts: []Post{
		{ExternalID: "demo-" + username + "-1", Username: username, Caption: "Bu içerik yalnızca arayüz testi için demo veridir.", Permalink: "https://www.instagram.com/p/demo1/", MediaType: "IMAGE", PublishedAt: now.Add(-time.Hour)},
		{ExternalID: "demo-" + username + "-2", Username: username, Caption: "Türkçe anahtar kelime araması için ikinci demo gönderi.", Permalink: "https://www.instagram.com/p/demo2/", MediaType: "VIDEO", PublishedAt: now.Add(-2 * time.Hour)},
	}}, nil
}
