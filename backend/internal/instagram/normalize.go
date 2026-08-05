package instagram

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var instagramUsername = regexp.MustCompile(`^[A-Za-z0-9._]{1,30}$`)
var reservedInstagramPaths = map[string]bool{"p": true, "reel": true, "reels": true, "stories": true, "explore": true, "accounts": true, "direct": true, "about": true, "developer": true}

// NormalizeProfile accepts a username, @username, or an instagram.com profile URL.
func NormalizeProfile(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", fmt.Errorf("%w: Instagram kullanıcı adı veya profil URL'si gerekli", ErrInvalidInput)
	}
	lowerValue := strings.ToLower(value)
	if strings.HasPrefix(lowerValue, "instagram.com/") || strings.HasPrefix(lowerValue, "www.instagram.com/") {
		value = "https://" + value
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("%w: profil URL'si geçersiz", ErrInvalidInput)
		}
		host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
		if host != "instagram.com" {
			return "", fmt.Errorf("%w: yalnızca instagram.com profil URL'leri kabul edilir", ErrInvalidInput)
		}
		parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
		if len(parts) != 1 || parts[0] == "" {
			return "", fmt.Errorf("%w: URL bir Instagram profilini göstermiyor", ErrInvalidInput)
		}
		decoded, err := url.PathUnescape(parts[0])
		if err != nil {
			return "", fmt.Errorf("%w: profil URL'si geçersiz", ErrInvalidInput)
		}
		value = decoded
	} else if strings.Contains(value, "/") || strings.Contains(value, "?") || strings.Contains(value, "#") {
		return "", fmt.Errorf("%w: Instagram kullanıcı adı geçersiz", ErrInvalidInput)
	}
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	value = strings.ToLower(value)
	if value == "" || reservedInstagramPaths[value] || !instagramUsername.MatchString(value) {
		return "", fmt.Errorf("%w: Instagram kullanıcı adı geçersiz", ErrInvalidInput)
	}
	return value, nil
}

func canonicalProfileURL(username string) string {
	return "https://www.instagram.com/" + username + "/"
}
