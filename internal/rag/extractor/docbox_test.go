package extractor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewDocboxExtractor_Defaults(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{})

	if ext.baseURL != "http://localhost:41273" {
		t.Errorf("expected default baseURL, got %s", ext.baseURL)
	}
}

func TestNewDocboxExtractor_CustomConfig(t *testing.T) {
	// Use a real resolvable domain for testing (validation does DNS lookup)
	ext := NewDocboxExtractor(DocboxConfig{
		BaseURL: "https://api.openai.com",
		Timeout: 60 * time.Second,
	})

	if ext.baseURL != "https://api.openai.com" {
		t.Errorf("expected custom baseURL, got %s", ext.baseURL)
	}
}

func TestNewDocboxExtractor_SSRFValidation(t *testing.T) {
	// Test that HTTP non-localhost URLs are rejected and fall back to default
	ext := NewDocboxExtractor(DocboxConfig{
		BaseURL: "http://malicious.attacker.com:8080",
		Timeout: 60 * time.Second,
	})

	// Should fall back to safe localhost due to SSRF validation
	if ext.baseURL != "http://localhost:41273" {
		t.Errorf("expected fallback to localhost for unsafe URL, got %s", ext.baseURL)
	}
}

func TestDocboxExtractor_SupportedFormats(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{})
	formats := ext.SupportedFormats()

	if len(formats) == 0 {
		t.Fatal("expected supported formats")
	}

	// Check for key formats
	expected := map[string]bool{
		".pdf":  false,
		".docx": false,
		".txt":  false,
		".md":   false,
		".html": false,
	}

	for _, f := range formats {
		if _, ok := expected[f]; ok {
			expected[f] = true
		}
	}

	for format, found := range expected {
		if !found {
			t.Errorf("expected format %s not in supported list", format)
		}
	}
}

func TestDocboxExtractor_IsSupported(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{})

	tests := []struct {
		filename string
		want     bool
	}{
		{"document.pdf", true},
		{"document.PDF", true},
		{"report.docx", true},
		{"notes.txt", true},
		{"readme.md", true},
		{"page.html", true},
		{"data.csv", true},
		{"book.epub", true},
		{"unknown.xyz", false},
		{"image.png", false},
		{"video.mp4", false},
	}

	for _, tt := range tests {
		got := ext.IsSupported(tt.filename)
		if got != tt.want {
			t.Errorf("IsSupported(%s) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}

func TestDocboxExtractor_Extract_PlainText(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{})

	content := "This is plain text content.\nWith multiple lines."
	reader := strings.NewReader(content)

	result, err := ext.Extract(context.Background(), reader, "test.txt", "text/plain")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Text != content {
		t.Errorf("expected text=%q, got %q", content, result.Text)
	}
	if result.PageCount != 1 {
		t.Errorf("expected PageCount=1, got %d", result.PageCount)
	}
	if result.Metadata["format"] != "plain" {
		t.Errorf("expected format=plain, got %v", result.Metadata["format"])
	}
}

func TestDocboxExtractor_Extract_Markdown(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{})

	content := "# Heading\n\nThis is **markdown** content."
	reader := strings.NewReader(content)

	result, err := ext.Extract(context.Background(), reader, "readme.md", "text/markdown")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Text != content {
		t.Errorf("expected text=%q, got %q", content, result.Text)
	}
	if result.Metadata["format"] != "markdown" {
		t.Errorf("expected format=markdown, got %v", result.Metadata["format"])
	}
}

func TestDocboxExtractor_Extract_UnsupportedFormat(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{})

	content := "binary-ish content that we try to read as text"
	reader := strings.NewReader(content)

	result, err := ext.Extract(context.Background(), reader, "unknown.xyz", "application/octet-stream")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Text != content {
		t.Errorf("expected text=%q, got %q", content, result.Text)
	}
	if result.Metadata["fallback"] != true {
		t.Error("expected fallback=true for unsupported format")
	}
}

func TestDocboxExtractor_Extract_PandocSuccess(t *testing.T) {
	extractedText := "This is the extracted text from the PDF document."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/convert" {
			t.Errorf("expected /convert path, got %s", r.URL.Path)
		}

		// Verify content type is multipart
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("expected multipart/form-data, got %s", contentType)
		}

		// Parse multipart form
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Errorf("failed to parse multipart form: %v", err)
		}

		// Verify form fields
		from := r.FormValue("from")
		to := r.FormValue("to")
		if from != "pdf" {
			t.Errorf("expected from=pdf, got %s", from)
		}
		if to != "plain" {
			t.Errorf("expected to=plain, got %s", to)
		}

		// Verify file was uploaded
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("expected file in form: %v", err)
		} else {
			defer file.Close()
			if header.Filename != "document.pdf" {
				t.Errorf("expected filename=document.pdf, got %s", header.Filename)
			}
		}

		// Return extracted text
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(extractedText))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})

	// Simulate PDF content
	pdfContent := "fake PDF binary content"
	reader := strings.NewReader(pdfContent)

	result, err := ext.Extract(context.Background(), reader, "document.pdf", "application/pdf")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Text != extractedText {
		t.Errorf("expected text=%q, got %q", extractedText, result.Text)
	}
	if result.Metadata["format"] != "pdf" {
		t.Errorf("expected format=pdf, got %v", result.Metadata["format"])
	}
	if result.Metadata["original_size"] != len(pdfContent) {
		t.Errorf("expected original_size=%d, got %v", len(pdfContent), result.Metadata["original_size"])
	}
}

func TestDocboxExtractor_Extract_Docx(t *testing.T) {
	extractedText := "Word document content."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20)
		from := r.FormValue("from")
		if from != "docx" {
			t.Errorf("expected from=docx, got %s", from)
		}
		w.Write([]byte(extractedText))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	reader := strings.NewReader("fake docx content")

	result, err := ext.Extract(context.Background(), reader, "report.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Text != extractedText {
		t.Errorf("expected text=%q, got %q", extractedText, result.Text)
	}
}

func TestDocboxExtractor_Extract_Html(t *testing.T) {
	extractedText := "HTML content without tags."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20)
		from := r.FormValue("from")
		if from != "html" {
			t.Errorf("expected from=html, got %s", from)
		}
		w.Write([]byte(extractedText))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	reader := strings.NewReader("<html><body>content</body></html>")

	result, err := ext.Extract(context.Background(), reader, "page.html", "text/html")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Text != extractedText {
		t.Errorf("expected text=%q, got %q", extractedText, result.Text)
	}
}

func TestDocboxExtractor_Extract_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	reader := strings.NewReader("pdf content")

	_, err := ext.Extract(context.Background(), reader, "doc.pdf", "application/pdf")

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code: %v", err)
	}
}

func TestDocboxExtractor_Extract_ErrorWithDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"detail": "Invalid file format"})
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	reader := strings.NewReader("bad content")

	_, err := ext.Extract(context.Background(), reader, "bad.pdf", "application/pdf")

	if err == nil {
		t.Fatal("expected error for bad request")
	}
	if !strings.Contains(err.Error(), "Invalid file format") {
		t.Errorf("error should contain detail message: %v", err)
	}
}

func TestDocboxExtractor_Extract_ConnectionError(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{
		BaseURL: "http://localhost:1",
		Timeout: 100 * time.Millisecond,
	})
	reader := strings.NewReader("content")

	_, err := ext.Extract(context.Background(), reader, "doc.pdf", "application/pdf")

	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestDocboxExtractor_Extract_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte("text"))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	reader := strings.NewReader("content")
	_, err := ext.Extract(ctx, reader, "doc.pdf", "application/pdf")

	if err == nil {
		t.Fatal("expected context canceled error")
	}
}

func TestDocboxExtractor_Extract_LargeDocument(t *testing.T) {
	// Generate text that would be ~3 pages
	largeText := strings.Repeat("This is a paragraph of text. ", 500) // ~15000 chars = 5 pages

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(largeText))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	reader := strings.NewReader("pdf content")

	result, err := ext.Extract(context.Background(), reader, "large.pdf", "application/pdf")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// ~15000 chars / 3000 = 5 pages
	if result.PageCount < 4 {
		t.Errorf("expected PageCount >= 4 for large document, got %d", result.PageCount)
	}
}

func TestDocboxExtractor_Extract_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("   "))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	reader := strings.NewReader("pdf content")

	result, err := ext.Extract(context.Background(), reader, "empty.pdf", "application/pdf")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Text != "" {
		t.Errorf("expected empty text after trim, got %q", result.Text)
	}
	// Empty text should still have PageCount 0 (due to integer division)
	if result.PageCount != 0 {
		t.Errorf("expected PageCount=0 for empty text, got %d", result.PageCount)
	}
}

func TestDocboxExtractor_Extract_WhitespaceHandling(t *testing.T) {
	textWithWhitespace := "  \n\n  Extracted content with whitespace.  \n\n  "
	expected := "Extracted content with whitespace."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(textWithWhitespace))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	reader := strings.NewReader("pdf content")

	result, err := ext.Extract(context.Background(), reader, "doc.pdf", "application/pdf")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Text != expected {
		t.Errorf("expected text=%q, got %q", expected, result.Text)
	}
}

func TestDocboxExtractor_Extract_ReadError(t *testing.T) {
	ext := NewDocboxExtractor(DocboxConfig{})

	// Create a reader that fails
	failReader := &failingReader{err: io.ErrUnexpectedEOF}

	_, err := ext.Extract(context.Background(), failReader, "doc.pdf", "application/pdf")

	if err == nil {
		t.Fatal("expected error from failing reader")
	}
}

// failingReader is a reader that always returns an error
type failingReader struct {
	err error
}

func (r *failingReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

func TestDocboxExtractor_CaseInsensitiveExtension(t *testing.T) {
	extractedText := "content"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(extractedText))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})

	// Test uppercase extension
	reader := strings.NewReader("content")
	result, err := ext.Extract(context.Background(), reader, "document.PDF", "application/pdf")

	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Text != extractedText {
		t.Errorf("expected text=%q, got %q", extractedText, result.Text)
	}
}

// Table-driven test for format mapping
func TestDocboxExtractor_FormatMapping(t *testing.T) {
	tests := []struct {
		filename       string
		expectedFormat string
	}{
		{"doc.pdf", "pdf"},
		{"doc.docx", "docx"},
		{"doc.doc", "docx"}, // .doc maps to docx
		{"doc.odt", "odt"},
		{"doc.rtf", "rtf"},
		{"doc.html", "html"},
		{"doc.htm", "html"},
		{"doc.epub", "epub"},
		{"doc.tex", "latex"},
		{"doc.rst", "rst"},
		{"doc.csv", "csv"},
		{"doc.json", "json"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			var receivedFormat string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseMultipartForm(10 << 20)
				receivedFormat = r.FormValue("from")
				w.Write([]byte("text"))
			}))
			defer server.Close()

			ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
			reader := strings.NewReader("content")

			_, err := ext.Extract(context.Background(), reader, tt.filename, "")
			if err != nil {
				t.Fatalf("Extract failed: %v", err)
			}

			if receivedFormat != tt.expectedFormat {
				t.Errorf("expected format=%s, got %s", tt.expectedFormat, receivedFormat)
			}
		})
	}
}

// Benchmark
func BenchmarkDocboxExtractor_Extract_PlainText(b *testing.B) {
	ext := NewDocboxExtractor(DocboxConfig{})
	content := strings.Repeat("This is test content. ", 100)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader(content)
		ext.Extract(ctx, reader, "test.txt", "text/plain")
	}
}

func BenchmarkDocboxExtractor_Extract_Pandoc(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("extracted text"))
	}))
	defer server.Close()

	ext := NewDocboxExtractor(DocboxConfig{BaseURL: server.URL})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := strings.NewReader("pdf content")
		ext.Extract(ctx, reader, "doc.pdf", "application/pdf")
	}
}
