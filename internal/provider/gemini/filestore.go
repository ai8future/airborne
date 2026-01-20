// Package gemini provides the Google Gemini LLM provider implementation.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ai8future/airborne/internal/validation"
)

const (
	fileSearchBaseURL         = "https://generativelanguage.googleapis.com/v1beta"
	filesAPIBaseURL           = "https://generativelanguage.googleapis.com/v1beta/files"
	fileSearchPollingInterval = 2 * time.Second
	fileSearchPollingTimeout  = 5 * time.Minute
)

// officeFileMIMETypes contains MIME types that require the Files API workaround.
// These types cannot be uploaded directly to FileSearchStore due to MIME type validation errors.
// Workaround: Upload to Files API first, then import into FileSearchStore.
var officeFileMIMETypes = map[string]bool{
	// Modern Office formats (OpenXML)
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true, // .xlsx
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true, // .docx
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true, // .pptx
	// Legacy Office formats
	"application/vnd.ms-excel":      true, // .xls
	"application/msword":            true, // .doc
	"application/vnd.ms-powerpoint": true, // .ppt
	// CSV
	"text/csv": true, // .csv
}

// isOfficeFile returns true if the MIME type requires the Files API workaround.
func isOfficeFile(mimeType string) bool {
	return officeFileMIMETypes[mimeType]
}

// FileStoreConfig contains configuration for Gemini file store operations.
type FileStoreConfig struct {
	APIKey  string
	BaseURL string // Optional override for testing
}

// FileStoreResult contains the result of a file store operation.
type FileStoreResult struct {
	StoreID       string
	Name          string
	Status        string
	DocumentCount int
	CreatedAt     time.Time
}

// UploadedFile contains information about an uploaded file.
type UploadedFile struct {
	FileID    string
	StoreID   string
	Filename  string
	Status    string
	Operation string
}

// fileSearchStoreResponse represents the API response for a FileSearchStore.
type fileSearchStoreResponse struct {
	Name                   string `json:"name"`
	DisplayName            string `json:"displayName"`
	CreateTime             string `json:"createTime"`
	UpdateTime             string `json:"updateTime"`
	TotalDocumentCount     int    `json:"totalDocumentCount"`
	ProcessedDocumentCount int    `json:"processedDocumentCount"`
	FailedDocumentCount    int    `json:"failedDocumentCount"`
	SizeBytes              string `json:"sizeBytes"`
}

// operationResponse represents a long-running operation response.
type operationResponse struct {
	Name     string                 `json:"name"`
	Done     bool                   `json:"done"`
	Error    *operationError        `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
}

// operationError represents an error from an operation.
type operationError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// getBaseURL returns the base URL for the FileSearch API.
func (cfg FileStoreConfig) getBaseURL() string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	return fileSearchBaseURL
}

// filesAPIResponse represents the response from the Files API upload.
type filesAPIResponse struct {
	File struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		MIMEType    string `json:"mimeType"`
		SizeBytes   string `json:"sizeBytes"`
		CreateTime  string `json:"createTime"`
		URI         string `json:"uri"`
		State       string `json:"state"`
	} `json:"file"`
}

// uploadToFilesAPI uploads a file to the Gemini Files API.
// This is the first step of the Office file workaround.
func uploadToFilesAPI(ctx context.Context, apiKey string, filename string, mimeType string, content []byte) (string, error) {
	// Create the upload URL
	uploadURL := fmt.Sprintf("https://generativelanguage.googleapis.com/upload/v1beta/files?key=%s", apiKey)

	// Create metadata JSON
	metadata := map[string]interface{}{
		"file": map[string]string{
			"displayName": filename,
		},
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	// Use resumable upload protocol for reliability
	// Step 1: Initiate the upload
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(metadataJSON))
	if err != nil {
		return "", fmt.Errorf("create init request: %w", err)
	}
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("X-Goog-Upload-Protocol", "resumable")
	initReq.Header.Set("X-Goog-Upload-Command", "start")
	initReq.Header.Set("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", len(content)))
	initReq.Header.Set("X-Goog-Upload-Header-Content-Type", mimeType)

	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		return "", fmt.Errorf("execute init request: %w", err)
	}
	defer initResp.Body.Close()

	if initResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(initResp.Body)
		return "", fmt.Errorf("init upload failed: %s - %s", initResp.Status, string(body))
	}

	// Get the upload URL from the response header
	resumableURL := initResp.Header.Get("X-Goog-Upload-URL")
	if resumableURL == "" {
		return "", fmt.Errorf("no resumable upload URL in response")
	}

	// Step 2: Upload the file content
	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, resumableURL, bytes.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	uploadReq.Header.Set("Content-Type", mimeType)
	uploadReq.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	uploadReq.Header.Set("X-Goog-Upload-Offset", "0")

	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("execute upload request: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uploadResp.Body)
		return "", fmt.Errorf("upload failed: %s - %s", uploadResp.Status, string(body))
	}

	var fileResp filesAPIResponse
	if err := json.NewDecoder(uploadResp.Body).Decode(&fileResp); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}

	slog.Info("file uploaded to Files API",
		"name", fileResp.File.Name,
		"display_name", fileResp.File.DisplayName,
		"mime_type", fileResp.File.MIMEType,
	)

	return fileResp.File.Name, nil
}

// importFileToFileSearchStore imports a file from the Files API into a FileSearchStore.
// This is the second step of the Office file workaround.
func importFileToFileSearchStore(ctx context.Context, cfg FileStoreConfig, storeID string, fileName string, displayName string) (*UploadedFile, error) {
	url := fmt.Sprintf("%s/fileSearchStores/%s:import?key=%s", cfg.getBaseURL(), storeID, cfg.APIKey)

	reqBody := map[string]interface{}{
		"inlinePassages": map[string]interface{}{
			"passages": []map[string]string{
				{
					"id":      fileName,
					"content": fmt.Sprintf("@%s", fileName), // Reference to the uploaded file
				},
			},
		},
	}

	// Actually, the import API expects a different format - use the files reference
	reqBody = map[string]interface{}{
		"sourceFiles": []map[string]string{
			{
				"file": fileName,
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	slog.Info("importing file to FileSearchStore",
		"store_id", storeID,
		"file_name", fileName,
		"display_name", displayName,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("import to file search store failed: %s - %s", resp.Status, string(body))
	}

	var opResp operationResponse
	if err := json.NewDecoder(resp.Body).Decode(&opResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	slog.Info("file import initiated",
		"store_id", storeID,
		"file_name", fileName,
		"operation", opResp.Name,
	)

	// Poll for completion
	status, err := waitForOperation(ctx, cfg, opResp.Name)
	if err != nil {
		slog.Warn("file import incomplete",
			"store_id", storeID,
			"file_name", fileName,
			"error", err,
		)
	}

	// Extract file ID
	fileID := ""
	if opResp.Response != nil {
		if name, ok := opResp.Response["name"].(string); ok {
			if idx := strings.LastIndex(name, "/"); idx != -1 {
				fileID = name[idx+1:]
			} else {
				fileID = name
			}
		}
	}

	return &UploadedFile{
		FileID:    fileID,
		StoreID:   storeID,
		Filename:  displayName,
		Status:    status,
		Operation: opResp.Name,
	}, nil
}

// deleteFromFilesAPI deletes a file from the Gemini Files API.
// This is used for cleanup after the Office file workaround.
func deleteFromFilesAPI(ctx context.Context, apiKey string, fileName string) error {
	url := fmt.Sprintf("%s/%s?key=%s", filesAPIBaseURL, fileName, apiKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// 200 OK or 404 Not Found are both acceptable
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete from Files API failed: %s - %s", resp.Status, string(body))
	}

	slog.Info("file deleted from Files API", "file_name", fileName)
	return nil
}

// CreateFileSearchStore creates a new Gemini FileSearchStore.
func CreateFileSearchStore(ctx context.Context, cfg FileStoreConfig, name string) (*FileStoreResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
	}

	url := fmt.Sprintf("%s/fileSearchStores?key=%s", cfg.getBaseURL(), cfg.APIKey)

	reqBody := map[string]string{}
	if name != "" {
		reqBody["displayName"] = name
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	slog.Info("creating gemini file search store", "name", name)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create file search store failed: %s - %s", resp.Status, string(body))
	}

	var storeResp fileSearchStoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&storeResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Extract store ID from name (format: fileSearchStores/xxx)
	storeID := storeResp.Name
	if idx := strings.LastIndex(storeResp.Name, "/"); idx != -1 {
		storeID = storeResp.Name[idx+1:]
	}

	slog.Info("gemini file search store created",
		"store_id", storeID,
		"name", storeResp.DisplayName,
	)

	createdAt, _ := time.Parse(time.RFC3339, storeResp.CreateTime)

	return &FileStoreResult{
		StoreID:       storeID,
		Name:          storeResp.DisplayName,
		Status:        "ready",
		DocumentCount: storeResp.TotalDocumentCount,
		CreatedAt:     createdAt,
	}, nil
}

// UploadFileToFileSearchStore uploads a file to a Gemini FileSearchStore.
// For Office files (DOCX, XLSX, PPTX, CSV), uses the Files API workaround:
// 1. Upload to Files API first (accepts these MIME types)
// 2. Import into FileSearchStore from Files API
// 3. Cleanup the intermediate file from Files API
func UploadFileToFileSearchStore(ctx context.Context, cfg FileStoreConfig, storeID string, filename string, mimeType string, content io.Reader) (*UploadedFile, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("API key is required")
	}
	if strings.TrimSpace(storeID) == "" {
		return nil, fmt.Errorf("store ID is required")
	}

	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
	}

	// Read the file content
	fileContent, err := io.ReadAll(content)
	if err != nil {
		return nil, fmt.Errorf("read file content: %w", err)
	}

	// Check if this is an Office file that requires the workaround
	if isOfficeFile(mimeType) {
		slog.Info("using Files API workaround for Office file",
			"store_id", storeID,
			"filename", filename,
			"mime_type", mimeType,
		)
		return uploadOfficeFileToFileSearchStore(ctx, cfg, storeID, filename, mimeType, fileContent)
	}

	// Standard direct upload for non-Office files
	return uploadDirectToFileSearchStore(ctx, cfg, storeID, filename, mimeType, fileContent)
}

// uploadOfficeFileToFileSearchStore implements the two-step workaround for Office files.
func uploadOfficeFileToFileSearchStore(ctx context.Context, cfg FileStoreConfig, storeID string, filename string, mimeType string, fileContent []byte) (*UploadedFile, error) {
	// Step 1: Upload to Files API
	filesAPIName, err := uploadToFilesAPI(ctx, cfg.APIKey, filename, mimeType, fileContent)
	if err != nil {
		return nil, fmt.Errorf("upload to Files API: %w", err)
	}

	// Ensure cleanup of the Files API file
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cleanupErr := deleteFromFilesAPI(cleanupCtx, cfg.APIKey, filesAPIName); cleanupErr != nil {
			slog.Warn("failed to cleanup file from Files API",
				"file_name", filesAPIName,
				"error", cleanupErr,
			)
		}
	}()

	// Step 2: Import from Files API to FileSearchStore
	result, err := importFileToFileSearchStore(ctx, cfg, storeID, filesAPIName, filename)
	if err != nil {
		return nil, fmt.Errorf("import to FileSearchStore: %w", err)
	}

	return result, nil
}

// uploadDirectToFileSearchStore performs a direct upload to FileSearchStore (for non-Office files).
func uploadDirectToFileSearchStore(ctx context.Context, cfg FileStoreConfig, storeID string, filename string, mimeType string, fileContent []byte) (*UploadedFile, error) {
	// Use the upload endpoint with multipart
	baseURL := cfg.getBaseURL()
	// Replace /v1beta with /upload/v1beta for media upload
	if strings.Contains(baseURL, "/v1beta") {
		baseURL = strings.Replace(baseURL, "/v1beta", "/upload/v1beta", 1)
	} else {
		baseURL = strings.Replace(baseURL, fileSearchBaseURL, fileSearchBaseURL+"/upload", 1)
	}

	url := fmt.Sprintf("%s/fileSearchStores/%s:uploadToFileSearchStore?key=%s", baseURL, storeID, cfg.APIKey)

	slog.Info("uploading file to gemini file search store (direct)",
		"store_id", storeID,
		"filename", filename,
		"mime_type", mimeType,
	)

	// Create multipart request
	// For Gemini upload, we need to send metadata as JSON and file as binary
	// Using simple JSON metadata with file in body
	metadataURL := fmt.Sprintf("%s/fileSearchStores/%s:uploadToFileSearchStore?key=%s", cfg.getBaseURL(), storeID, cfg.APIKey)

	reqBody := map[string]interface{}{
		"displayName": filename,
		"mimeType":    mimeType,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	// First, try the simple upload approach with metadata
	// Create a combined request body for the upload
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(fileContent))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if mimeType != "" {
		req.Header.Set("Content-Type", mimeType)
	}
	req.Header.Set("X-Goog-Upload-Protocol", "raw")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute upload request: %w", err)
	}
	defer resp.Body.Close()

	// If raw upload fails, try JSON metadata approach
	if resp.StatusCode != http.StatusOK {
		// Try JSON metadata approach
		req2, err := http.NewRequestWithContext(ctx, http.MethodPost, metadataURL, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("create metadata request: %w", err)
		}
		req2.Header.Set("Content-Type", "application/json")

		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			return nil, fmt.Errorf("execute metadata request: %w", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp2.Body)
			return nil, fmt.Errorf("upload to file search store failed: %s - %s", resp2.Status, string(body))
		}
		resp = resp2
	}

	var opResp operationResponse
	if err := json.NewDecoder(resp.Body).Decode(&opResp); err != nil {
		return nil, fmt.Errorf("decode operation response: %w", err)
	}

	slog.Info("file upload initiated",
		"store_id", storeID,
		"filename", filename,
		"operation", opResp.Name,
	)

	// Poll for completion
	status, err := waitForOperation(ctx, cfg, opResp.Name)
	if err != nil {
		slog.Warn("file processing incomplete",
			"store_id", storeID,
			"filename", filename,
			"error", err,
		)
	}

	// Extract file ID from operation response
	fileID := ""
	if opResp.Response != nil {
		if name, ok := opResp.Response["name"].(string); ok {
			if idx := strings.LastIndex(name, "/"); idx != -1 {
				fileID = name[idx+1:]
			} else {
				fileID = name
			}
		}
	}

	return &UploadedFile{
		FileID:    fileID,
		StoreID:   storeID,
		Filename:  filename,
		Status:    status,
		Operation: opResp.Name,
	}, nil
}

// waitForOperation polls until an operation completes.
func waitForOperation(ctx context.Context, cfg FileStoreConfig, operationName string) (string, error) {
	if operationName == "" {
		return "unknown", nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, fileSearchPollingTimeout)
	defer cancel()

	ticker := time.NewTicker(fileSearchPollingInterval)
	defer ticker.Stop()

	url := fmt.Sprintf("%s/%s?key=%s", cfg.getBaseURL(), operationName, cfg.APIKey)

	for {
		select {
		case <-timeoutCtx.Done():
			return "in_progress", fmt.Errorf("operation timeout")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, url, nil)
			if err != nil {
				return "unknown", fmt.Errorf("create request: %w", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "unknown", fmt.Errorf("execute request: %w", err)
			}

			var opResp operationResponse
			if err := json.NewDecoder(resp.Body).Decode(&opResp); err != nil {
				resp.Body.Close()
				return "unknown", fmt.Errorf("decode response: %w", err)
			}
			resp.Body.Close()

			if opResp.Done {
				if opResp.Error != nil {
					return "failed", fmt.Errorf("operation failed: %s", opResp.Error.Message)
				}
				return "completed", nil
			}
		}
	}
}

// DeleteFileSearchStore deletes a Gemini FileSearchStore.
func DeleteFileSearchStore(ctx context.Context, cfg FileStoreConfig, storeID string, force bool) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("API key is required")
	}
	if strings.TrimSpace(storeID) == "" {
		return fmt.Errorf("store ID is required")
	}

	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return fmt.Errorf("invalid base URL: %w", err)
		}
	}

	url := fmt.Sprintf("%s/fileSearchStores/%s?key=%s", cfg.getBaseURL(), storeID, cfg.APIKey)
	if force {
		url += "&force=true"
	}

	slog.Info("deleting gemini file search store", "store_id", storeID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete file search store failed: %s - %s", resp.Status, string(body))
	}

	slog.Info("gemini file search store deleted", "store_id", storeID)
	return nil
}

// GetFileSearchStore retrieves information about a Gemini FileSearchStore.
func GetFileSearchStore(ctx context.Context, cfg FileStoreConfig, storeID string) (*FileStoreResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("API key is required")
	}
	if strings.TrimSpace(storeID) == "" {
		return nil, fmt.Errorf("store ID is required")
	}

	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
	}

	url := fmt.Sprintf("%s/fileSearchStores/%s?key=%s", cfg.getBaseURL(), storeID, cfg.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get file search store failed: %s - %s", resp.Status, string(body))
	}

	var storeResp fileSearchStoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&storeResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Determine status based on document counts
	status := "ready"
	if storeResp.ProcessedDocumentCount < storeResp.TotalDocumentCount {
		status = "processing"
	}
	if storeResp.FailedDocumentCount > 0 {
		status = "partial"
	}

	createdAt, _ := time.Parse(time.RFC3339, storeResp.CreateTime)

	return &FileStoreResult{
		StoreID:       storeID,
		Name:          storeResp.DisplayName,
		Status:        status,
		DocumentCount: storeResp.TotalDocumentCount,
		CreatedAt:     createdAt,
	}, nil
}

// ListFileSearchStores lists all FileSearchStores.
func ListFileSearchStores(ctx context.Context, cfg FileStoreConfig, limit int) ([]FileStoreResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
	}

	url := fmt.Sprintf("%s/fileSearchStores?key=%s", cfg.getBaseURL(), cfg.APIKey)
	if limit > 0 && limit <= 20 {
		url += fmt.Sprintf("&pageSize=%d", limit)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list file search stores failed: %s - %s", resp.Status, string(body))
	}

	var listResp struct {
		FileSearchStores []fileSearchStoreResponse `json:"fileSearchStores"`
		NextPageToken    string                    `json:"nextPageToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var results []FileStoreResult
	for _, store := range listResp.FileSearchStores {
		storeID := store.Name
		if idx := strings.LastIndex(store.Name, "/"); idx != -1 {
			storeID = store.Name[idx+1:]
		}

		createdAt, _ := time.Parse(time.RFC3339, store.CreateTime)

		results = append(results, FileStoreResult{
			StoreID:       storeID,
			Name:          store.DisplayName,
			Status:        "ready",
			DocumentCount: store.TotalDocumentCount,
			CreatedAt:     createdAt,
		})
	}

	return results, nil
}
