package instagram

import "testing"

func TestNormalizeProfile(t *testing.T) {
	tests := []struct{ input, want string }{
		{"omereski", "omereski"},
		{" @OmerEski ", "omereski"},
		{"https://instagram.com/omereski", "omereski"},
		{"https://www.instagram.com/omereski/", "omereski"},
		{"https://www.instagram.com/omereski/?hl=tr", "omereski"},
	}
	for _, tt := range tests {
		got, err := NormalizeProfile(tt.input)
		if err != nil || got != tt.want {
			t.Errorf("NormalizeProfile(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
		}
	}
}

func TestNormalizeProfileRejectsNonProfiles(t *testing.T) {
	for _, input := range []string{"", "https://instagram.com/p/abc", "https://instagram.com/reel/abc", "https://instagram.com/stories/user/1", "https://google.com/omereski", "bad/name"} {
		if got, err := NormalizeProfile(input); err == nil {
			t.Errorf("NormalizeProfile(%q) = %q; expected error", input, got)
		}
	}
}
