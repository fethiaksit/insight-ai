package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	MaxFiles           = 50
	DefaultMaxFileSize = int64(25 << 20)
	multipartOverhead  = int64(10 << 20)
)

var (
	ErrNotFound = errors.New("belge bulunamadı")
	ErrInvalid  = errors.New("geçersiz belge isteği")
	ErrTooLarge = errors.New("PDF dosyası 25 MB sınırını aşıyor")
)

type Document struct {
	ID             string    `json:"id"`
	Filename       string    `json:"filename"`
	StoredFilename string    `json:"stored_filename"`
	PageCount      int       `json:"page_count"`
	CreatedAt      time.Time `json:"created_at"`
}

type Page struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}

type SearchResult struct {
	DocumentID      string   `json:"document_id"`
	Filename        string   `json:"filename"`
	Page            int      `json:"page"`
	Snippet         string   `json:"snippet"`
	MatchedKeywords []string `json:"matched_keywords"`
}

type extractionResponse struct {
	Filename  string `json:"filename"`
	PageCount int    `json:"page_count"`
	Pages     []Page `json:"pages"`
}

type Service struct {
	redis        *redis.Client
	extractorURL string
	storageDir   string
	maxFileSize  int64
	httpClient   *http.Client
}

func NewService(client *redis.Client, extractorBaseURL, storageDir string, _ time.Duration) *Service {
	return &Service{
		redis:        client,
		extractorURL: strings.TrimRight(extractorBaseURL, "/") + "/v1/pdf/extract",
		storageDir:   storageDir,
		maxFileSize:  DefaultMaxFileSize,
		httpClient:   &http.Client{Timeout: 0},
	}
}

func RegisterRoutes(group *gin.RouterGroup, service *Service) {
	group.POST("/upload", service.upload)
	group.GET("", service.list)
	group.GET("/search", service.search)
	group.GET("/:id", service.get)
	group.GET("/:id/file", service.file)
	group.DELETE("/:id", service.delete)
}

func (s *Service) upload(c *gin.Context) {
	maxRequestSize := s.maxFileSize*MaxFiles + multipartOverhead
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestSize)
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(c, http.StatusRequestEntityTooLarge, "Toplam yükleme boyutu sınırı aşıldı")
			return
		}
		writeError(c, http.StatusBadRequest, "Geçerli bir multipart/form-data isteği gerekli")
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}

	files := c.Request.MultipartForm.File["files"]
	if len(files) == 0 {
		files = c.Request.MultipartForm.File["file"]
	}
	if len(files) == 0 {
		writeError(c, http.StatusBadRequest, "En az bir PDF dosyası gerekli")
		return
	}
	if len(files) > MaxFiles {
		writeError(c, http.StatusBadRequest, "Bir istekte en fazla 50 PDF yüklenebilir")
		return
	}
	for _, file := range files {
		if err := s.validateFile(file); err != nil {
			writeServiceError(c, fmt.Errorf("%s: %w", filepath.Base(file.Filename), err))
			return
		}
	}

	created := make([]Document, 0, len(files))
	for _, file := range files {
		document, err := s.save(c.Request.Context(), file)
		if err != nil {
			for i := range created {
				_ = s.remove(c.Request.Context(), created[i])
			}
			writeServiceError(c, fmt.Errorf("%s yüklenemedi: %w", filepath.Base(file.Filename), err))
			return
		}
		created = append(created, document)
	}
	c.JSON(http.StatusCreated, created)
}

func (s *Service) validateFile(header *multipart.FileHeader) error {
	if !strings.EqualFold(filepath.Ext(header.Filename), ".pdf") {
		return fmt.Errorf("%w: yalnızca PDF dosyaları kabul edilir", ErrInvalid)
	}
	if header.Size <= 0 {
		return fmt.Errorf("%w: dosya boş", ErrInvalid)
	}
	if header.Size > s.maxFileSize {
		return ErrTooLarge
	}
	file, err := header.Open()
	if err != nil {
		return err
	}
	defer file.Close()
	buffer := make([]byte, 1024)
	n, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if !bytes.Contains(buffer[:n], []byte("%PDF-")) {
		return fmt.Errorf("%w: dosya içeriği PDF değil", ErrInvalid)
	}
	return nil
}

func (s *Service) save(ctx context.Context, header *multipart.FileHeader) (Document, error) {
	if err := os.MkdirAll(s.storageDir, 0o750); err != nil {
		return Document{}, fmt.Errorf("belge klasörü oluşturulamadı: %w", err)
	}
	id := uuid.NewString()
	storedFilename := id + ".pdf"
	path := filepath.Join(s.storageDir, storedFilename)
	if err := s.copyFile(header, path); err != nil {
		return Document{}, err
	}

	extraction, err := s.extract(ctx, path, filepath.Base(header.Filename))
	if err != nil {
		_ = os.Remove(path)
		return Document{}, err
	}
	document := Document{
		ID:             id,
		Filename:       filepath.Base(header.Filename),
		StoredFilename: storedFilename,
		PageCount:      extraction.PageCount,
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.persist(ctx, document, extraction.Pages); err != nil {
		_ = os.Remove(path)
		return Document{}, err
	}
	return document, nil
}

func (s *Service) copyFile(header *multipart.FileHeader, destination string) error {
	source, err := header.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		_ = destinationFile.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()
	written, err := io.Copy(destinationFile, io.LimitReader(source, s.maxFileSize+1))
	if err != nil {
		return err
	}
	if written > s.maxFileSize {
		return ErrTooLarge
	}
	if err := destinationFile.Sync(); err != nil {
		return err
	}
	success = true
	return nil
}

func (s *Service) extract(ctx context.Context, path, filename string) (extractionResponse, error) {
	file, err := os.Open(path)
	if err != nil {
		return extractionResponse{}, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return extractionResponse{}, err
	}
	if _, err = io.Copy(part, file); err != nil {
		return extractionResponse{}, err
	}
	if err = writer.Close(); err != nil {
		return extractionResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.extractorURL, &body)
	if err != nil {
		return extractionResponse{}, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := s.httpClient.Do(request)
	if err != nil {
		return extractionResponse{}, fmt.Errorf("PDF extraction servisine ulaşılamadı: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		extractionErr := fmt.Errorf("PDF extraction servisi %d döndürdü: %s", response.StatusCode, strings.TrimSpace(string(message)))
		if response.StatusCode == http.StatusBadRequest {
			return extractionResponse{}, fmt.Errorf("%w: %v", ErrInvalid, extractionErr)
		}
		if response.StatusCode == http.StatusRequestEntityTooLarge {
			return extractionResponse{}, fmt.Errorf("%w: %v", ErrTooLarge, extractionErr)
		}
		return extractionResponse{}, extractionErr
	}
	var extraction extractionResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 128<<20))
	if err := decoder.Decode(&extraction); err != nil {
		return extractionResponse{}, fmt.Errorf("PDF extraction yanıtı okunamadı: %w", err)
	}
	if extraction.PageCount < 1 || extraction.PageCount != len(extraction.Pages) {
		return extractionResponse{}, errors.New("PDF extraction servisi geçersiz sayfa verisi döndürdü")
	}
	for index, page := range extraction.Pages {
		if page.Page != index+1 {
			return extractionResponse{}, errors.New("PDF extraction servisi geçersiz sayfa sırası döndürdü")
		}
	}
	return extraction, nil
}

func (s *Service) persist(ctx context.Context, document Document, pages []Page) error {
	pipe := s.redis.TxPipeline()
	pipe.HSet(ctx, documentKey(document.ID), map[string]interface{}{
		"id":              document.ID,
		"filename":        document.Filename,
		"stored_filename": document.StoredFilename,
		"page_count":      document.PageCount,
		"created_at":      document.CreatedAt.Format(time.RFC3339Nano),
	})
	for _, page := range pages {
		pipe.HSet(ctx, pageKey(document.ID, page.Page), map[string]interface{}{
			"page":            page.Page,
			"text":            page.Text,
			"normalized_text": Normalize(page.Text),
		})
	}
	pipe.SAdd(ctx, "documents", document.ID)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Service) list(c *gin.Context) {
	documents, err := s.documents(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, documents)
}

func (s *Service) documents(ctx context.Context) ([]Document, error) {
	ids, err := s.redis.SMembers(ctx, "documents").Result()
	if err != nil {
		return nil, err
	}
	documents := make([]Document, 0, len(ids))
	for _, id := range ids {
		document, err := s.document(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	sort.Slice(documents, func(i, j int) bool {
		return documents[i].CreatedAt.After(documents[j].CreatedAt)
	})
	return documents, nil
}

func (s *Service) get(c *gin.Context) {
	document, err := s.document(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, document)
}

func (s *Service) document(ctx context.Context, id string) (Document, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Document{}, ErrNotFound
	}
	values, err := s.redis.HGetAll(ctx, documentKey(id)).Result()
	if err != nil {
		return Document{}, err
	}
	if len(values) == 0 {
		return Document{}, ErrNotFound
	}
	pageCount, err := strconv.Atoi(values["page_count"])
	if err != nil {
		return Document{}, fmt.Errorf("belge metadata sayfa sayısı geçersiz: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, values["created_at"])
	if err != nil {
		return Document{}, fmt.Errorf("belge metadata tarihi geçersiz: %w", err)
	}
	return Document{ID: values["id"], Filename: values["filename"], StoredFilename: values["stored_filename"], PageCount: pageCount, CreatedAt: createdAt}, nil
}

func (s *Service) file(c *gin.Context) {
	document, err := s.document(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if filepath.Base(document.StoredFilename) != document.StoredFilename {
		writeServiceError(c, errors.New("belge dosya adı geçersiz"))
		return
	}
	path := filepath.Join(s.storageDir, document.StoredFilename)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeServiceError(c, ErrNotFound)
		} else {
			writeServiceError(c, err)
		}
		return
	}
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": document.Filename}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(path)
}

func (s *Service) delete(c *gin.Context) {
	document, err := s.document(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if err := s.remove(c.Request.Context(), document); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Service) remove(ctx context.Context, document Document) error {
	pipe := s.redis.TxPipeline()
	for page := 1; page <= document.PageCount; page++ {
		pipe.Del(ctx, pageKey(document.ID, page))
	}
	pipe.Del(ctx, documentKey(document.ID))
	pipe.SRem(ctx, "documents", document.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	if filepath.Base(document.StoredFilename) != document.StoredFilename {
		return errors.New("belge dosya adı geçersiz")
	}
	if err := os.Remove(filepath.Join(s.storageDir, document.StoredFilename)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Service) search(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		writeError(c, http.StatusBadRequest, "q parametresi gerekli")
		return
	}
	match := strings.ToLower(c.DefaultQuery("match", "any"))
	if match != "any" && match != "all" {
		writeError(c, http.StatusBadRequest, "match yalnızca any veya all olabilir")
		return
	}
	originalKeywords, normalizedKeywords := keywords(query)
	if len(normalizedKeywords) == 0 {
		writeError(c, http.StatusBadRequest, "En az bir anahtar kelime gerekli")
		return
	}
	documents, err := s.documents(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	results := make([]SearchResult, 0)
	for _, document := range documents {
		for pageNumber := 1; pageNumber <= document.PageCount; pageNumber++ {
			values, err := s.redis.HGetAll(c.Request.Context(), pageKey(document.ID, pageNumber)).Result()
			if err != nil {
				writeServiceError(c, err)
				return
			}
			if len(values) == 0 {
				continue
			}
			matched := matchedKeywords(values["normalized_text"], originalKeywords, normalizedKeywords)
			if len(matched) == 0 || match == "all" && len(matched) != len(normalizedKeywords) {
				continue
			}
			results = append(results, SearchResult{
				DocumentID:      document.ID,
				Filename:        document.Filename,
				Page:            pageNumber,
				Snippet:         makeSnippet(values["text"], normalizedKeywords),
				MatchedKeywords: matched,
			})
		}
	}
	c.JSON(http.StatusOK, results)
}

func keywords(query string) ([]string, []string) {
	original := make([]string, 0)
	normalized := make([]string, 0)
	seen := make(map[string]struct{})
	for _, keyword := range strings.Fields(query) {
		normalizedKeyword := Normalize(keyword)
		if normalizedKeyword == "" {
			continue
		}
		if _, exists := seen[normalizedKeyword]; exists {
			continue
		}
		seen[normalizedKeyword] = struct{}{}
		original = append(original, keyword)
		normalized = append(normalized, normalizedKeyword)
	}
	return original, normalized
}

func matchedKeywords(normalizedText string, original, normalized []string) []string {
	matched := make([]string, 0, len(normalized))
	for index, keyword := range normalized {
		if strings.Contains(normalizedText, keyword) {
			matched = append(matched, original[index])
		}
	}
	return matched
}

func makeSnippet(text string, normalizedKeywords []string) string {
	textRunes := []rune(text)
	normalizedRunes := []rune(Normalize(text))
	firstMatch := -1
	matchLength := 0
	for _, keyword := range normalizedKeywords {
		index := runeIndex(normalizedRunes, []rune(keyword))
		if index >= 0 && (firstMatch == -1 || index < firstMatch) {
			firstMatch = index
			matchLength = utf8.RuneCountInString(keyword)
		}
	}
	if firstMatch < 0 || len(textRunes) == 0 {
		return ""
	}
	start := firstMatch - 100
	if start < 0 {
		start = 0
	}
	end := firstMatch + matchLength + 100
	if end > len(textRunes) {
		end = len(textRunes)
	}
	snippet := strings.TrimSpace(string(textRunes[start:end]))
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(textRunes) {
		snippet += "…"
	}
	return snippet
}

func runeIndex(text, search []rune) int {
	if len(search) == 0 || len(search) > len(text) {
		return -1
	}
	for start := 0; start <= len(text)-len(search); start++ {
		matched := true
		for offset := range search {
			if text[start+offset] != search[offset] {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}

func Normalize(value string) string {
	replacer := strings.NewReplacer(
		"İ", "i", "I", "i", "ı", "i",
		"Ş", "s", "ş", "s", "Ğ", "g", "ğ", "g",
		"Ü", "u", "ü", "u", "Ö", "o", "ö", "o",
		"Ç", "c", "ç", "c",
	)
	return strings.ToLower(replacer.Replace(value))
}

func documentKey(id string) string { return "document:" + id }
func pageKey(id string, page int) string {
	return fmt.Sprintf("document:%s:page:%d", id, page)
}

func writeServiceError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, ErrTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	case strings.Contains(err.Error(), "extraction servisi"):
		status = http.StatusBadGateway
	}
	writeError(c, status, err.Error())
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": gin.H{"message": message}})
}
