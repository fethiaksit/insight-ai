package documents

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizeTurkishCharacters(t *testing.T) {
	got := Normalize("İZMİR Belediyesi IĞDIR şğüöç")
	want := "izmir belediyesi igdir sguoc"
	if got != want {
		t.Fatalf("Normalize() = %q, want %q", got, want)
	}
}

func TestKeywordsAndMatching(t *testing.T) {
	original, normalized := keywords("İZMİR belediyesi izmir")
	if len(normalized) != 2 || normalized[0] != "izmir" || normalized[1] != "belediyesi" {
		t.Fatalf("unexpected normalized keywords: %#v", normalized)
	}
	matched := matchedKeywords("izmir buyuksehir belediyesi", original, normalized)
	if len(matched) != 2 || matched[0] != "İZMİR" || matched[1] != "belediyesi" {
		t.Fatalf("unexpected matched keywords: %#v", matched)
	}
}

func TestSnippetSurroundsFirstMatch(t *testing.T) {
	text := strings.Repeat("önce ", 40) + "İZMİR Belediyesi" + strings.Repeat(" sonra", 40)
	snippet := makeSnippet(text, []string{"izmir", "belediyesi"})
	if !strings.Contains(snippet, "İZMİR Belediyesi") {
		t.Fatalf("snippet does not contain match: %q", snippet)
	}
	if len([]rune(snippet)) > 220 {
		t.Fatalf("snippet is too long: %d runes", len([]rune(snippet)))
	}
}

func TestUploadRejectsNonPDFContent(t *testing.T) {
	response := uploadRequest(t, 1, []byte("not a pdf"), "sahte.pdf")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestUploadRejectsMoreThanFiftyFiles(t *testing.T) {
	response := uploadRequest(t, 51, []byte("%PDF-1.7"), "ornek.pdf")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func uploadRequest(t *testing.T, count int, content []byte, filename string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for range count {
		part, err := writer.CreateFormFile("files", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/documents/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	router := gin.New()
	RegisterRoutes(router.Group("/api/documents"), &Service{maxFileSize: DefaultMaxFileSize})
	router.ServeHTTP(response, request)
	return response
}
