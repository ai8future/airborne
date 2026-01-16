// Package openai provides the OpenAI LLM provider implementation using the Responses API.
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"

	"github.com/ai8future/airborne/internal/httpcapture"
	"github.com/ai8future/airborne/internal/provider"
	"github.com/ai8future/airborne/internal/retry"
	"github.com/ai8future/airborne/internal/validation"
)

const (
	pollInitial = 500 * time.Millisecond
	pollMax     = 5 * time.Second
)

// citationMarkerPattern matches OpenAI's inline file citation markers like "fileciteturn2file0"
var citationMarkerPattern = regexp.MustCompile(`filecite(?:turn\d+file\d+)+`)

// Client implements the provider.Provider interface using OpenAI's Responses API.
type Client struct {
	debug bool
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithDebugLogging enables verbose OpenAI payload logging.
func WithDebugLogging(enabled bool) ClientOption {
	return func(c *Client) {
		c.debug = enabled
	}
}

// NewClient creates a new OpenAI provider client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// Name returns the provider identifier.
func (c *Client) Name() string {
	return "openai"
}

// SupportsFileSearch returns true as OpenAI supports vector store file search.
func (c *Client) SupportsFileSearch() bool {
	return true
}

// SupportsWebSearch returns true as OpenAI supports web search preview.
func (c *Client) SupportsWebSearch() bool {
	return true
}

// SupportsNativeContinuity returns true as OpenAI supports previousResponseID.
func (c *Client) SupportsNativeContinuity() bool {
	return true
}

// SupportsStreaming returns true as OpenAI supports streaming responses.
func (c *Client) SupportsStreaming() bool {
	return true
}

// GenerateReply implements provider.Provider using OpenAI's Responses API.
func (c *Client) GenerateReply(ctx context.Context, params provider.GenerateParams) (provider.GenerateResult, error) {
	// Ensure request has a timeout
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, retry.RequestTimeout)
		defer cancel()
	}

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		return provider.GenerateResult{}, errors.New("OpenAI API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Create capturing transport for debug JSON (only when debug enabled)
	var capture *httpcapture.Transport
	clientOpts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if c.debug {
		capture = httpcapture.New()
		clientOpts = append(clientOpts, option.WithHTTPClient(capture.Client()))
	}
	if cfg.BaseURL != "" {
		// SECURITY: Validate base URL to prevent SSRF attacks
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			return provider.GenerateResult{}, fmt.Errorf("invalid base URL: %w", err)
		}
		clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
	}

	client := openai.NewClient(clientOpts...)

	// Build user prompt from input and history
	userPrompt := buildUserPrompt(params.UserInput, params.ConversationHistory)

	// Build request
	req := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(model),
		Instructions: openai.String(params.Instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Background: openai.Bool(true),
	}

	// Apply optional parameters
	if cfg.Temperature != nil {
		req.Temperature = openai.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		req.TopP = openai.Float(*cfg.TopP)
	}
	if cfg.MaxOutputTokens != nil {
		req.MaxOutputTokens = openai.Int(int64(*cfg.MaxOutputTokens))
	}

	// Apply reasoning effort
	if effort := cfg.ExtraOptions["reasoning_effort"]; effort != "" {
		req.Reasoning = shared.ReasoningParam{
			Effort: mapReasoningEffort(effort),
		}
	}

	// Apply service tier
	if tier := cfg.ExtraOptions["service_tier"]; tier != "" {
		req.ServiceTier = mapServiceTier(tier)
	}

	// Apply verbosity setting
	if verbosity := cfg.ExtraOptions["verbosity"]; verbosity != "" {
		textConfig := responses.ResponseTextConfigParam{}
		textConfig.SetExtraFields(map[string]any{
			"verbosity": strings.ToLower(verbosity),
		})
		req.Text = textConfig
	}

	// Apply prompt cache retention for gpt-5.x models
	if supportsPromptCacheRetention(model) {
		retention := cfg.ExtraOptions["prompt_cache_retention"]
		if retention == "" {
			retention = "24h"
		}
		req.SetExtraFields(map[string]any{
			"prompt_cache_retention": retention,
		})
	}

	// Build tools
	var tools []responses.ToolUnionParam
	if params.EnableFileSearch && strings.TrimSpace(params.FileStoreID) != "" {
		tools = append(tools, responses.ToolUnionParam{
			OfFileSearch: &responses.FileSearchToolParam{
				Type:           constant.FileSearch("file_search"),
				VectorStoreIDs: []string{params.FileStoreID},
			},
		})
	}
	if params.EnableWebSearch {
		tools = append(tools, responses.ToolUnionParam{
			OfWebSearchPreview: &responses.WebSearchToolParam{
				Type:              responses.WebSearchToolTypeWebSearchPreview,
				SearchContextSize: responses.WebSearchToolSearchContextSizeMedium,
			},
		})
	}
	if params.EnableCodeExecution {
		tools = append(tools, responses.ToolUnionParam{
			OfCodeInterpreter: &responses.ToolCodeInterpreterParam{
				Type: constant.CodeInterpreter("code_interpreter"),
				Container: responses.ToolCodeInterpreterContainerUnionParam{
					OfCodeInterpreterContainerAuto: &responses.ToolCodeInterpreterContainerCodeInterpreterContainerAutoParam{
						Type: constant.Auto("auto"),
					},
				},
			},
		})
	}
	// Add custom function tools
	for _, tool := range params.Tools {
		tools = append(tools, buildFunctionTool(tool))
	}
	if len(tools) > 0 {
		req.Tools = tools
	}

	// Add previous response ID for conversation continuity
	if strings.TrimSpace(params.PreviousResponseID) != "" {
		req.PreviousResponseID = openai.String(params.PreviousResponseID)
	}

	if c.debug {
		slog.Debug("openai request",
			"model", model,
			"override_model", params.OverrideModel,
			"file_store_id", params.FileStoreID,
			"request_id", params.RequestID,
		)
	}

	// Execute with retry
	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		slog.Info("openai request",
			"attempt", attempt,
			"model", model,
			"request_id", params.RequestID,
		)

		reqCtx, reqCancel := context.WithTimeout(ctx, retry.RequestTimeout)
		resp, err := client.Responses.New(reqCtx, req)
		reqCancel()

		if err != nil {
			// Check if parent context is still valid
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				lastErr = fmt.Errorf("openai request timeout: %w", err)
				slog.Warn("openai timeout, retrying", "attempt", attempt)
				if attempt < retry.MaxAttempts {
					retry.SleepWithBackoff(ctx, attempt)
					continue
				}
				return provider.GenerateResult{}, lastErr
			}

			lastErr = fmt.Errorf("openai error: %w", err)
			if !isRetryableError(err) {
				return provider.GenerateResult{}, lastErr
			}

			slog.Warn("openai retryable error", "attempt", attempt, "error", err)
			if attempt < retry.MaxAttempts {
				retry.SleepWithBackoff(ctx, attempt)
				continue
			}
			return provider.GenerateResult{}, lastErr
		}

		// Wait for completion
		resp, err = waitForCompletion(ctx, client, resp)
		if err != nil {
			lastErr = err
			slog.Warn("openai wait error", "attempt", attempt, "error", err)
			continue
		}

		text := strings.TrimSpace(resp.OutputText())
		if text == "" {
			lastErr = errors.New("openai returned empty response")
			continue
		}

		// Strip OpenAI's inline file citation markers (e.g., "fileciteturn2file0")
		text = stripCitationMarkers(text)

		citations := extractCitations(resp, params.FileIDToFilename)
		toolCalls := extractToolCalls(resp)
		codeExecutions := extractCodeExecutions(resp)

		slog.Info("openai request completed",
			"response_id", resp.ID,
			"model", model,
			"tokens_in", resp.Usage.InputTokens,
			"tokens_out", resp.Usage.OutputTokens,
			"tool_calls", len(toolCalls),
			"code_executions", len(codeExecutions),
		)

		var reqJSON, respJSON []byte
		if capture != nil {
			reqJSON = capture.RequestBody
			respJSON = capture.ResponseBody
		}

		return provider.GenerateResult{
			Text:       text,
			ResponseID: resp.ID,
			Usage: &provider.Usage{
				InputTokens:  resp.Usage.InputTokens,
				OutputTokens: resp.Usage.OutputTokens,
				TotalTokens:  resp.Usage.TotalTokens,
			},
			Citations:          citations,
			Model:              model,
			ToolCalls:          toolCalls,
			RequiresToolOutput: len(toolCalls) > 0,
			CodeExecutions:     codeExecutions,
			RequestJSON:        reqJSON,
			ResponseJSON:       respJSON,
		}, nil
	}

	return provider.GenerateResult{}, lastErr
}

// GenerateReplyStream implements streaming responses using OpenAI's Responses API.
func (c *Client) GenerateReplyStream(ctx context.Context, params provider.GenerateParams) (<-chan provider.StreamChunk, error) {
	// Ensure request has a timeout
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, retry.RequestTimeout)
	}

	// Helper to clean up cancel on error returns
	cleanup := func() {
		if cancel != nil {
			cancel()
		}
	}

	cfg := params.Config

	if strings.TrimSpace(cfg.APIKey) == "" {
		cleanup()
		return nil, errors.New("OpenAI API key is required")
	}

	model := cfg.Model
	if model == "" {
		model = "gpt-4o"
	}
	if strings.TrimSpace(params.OverrideModel) != "" {
		model = params.OverrideModel
	}

	// Build client options
	clientOpts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		if err := validation.ValidateProviderURL(cfg.BaseURL); err != nil {
			cleanup()
			return nil, fmt.Errorf("invalid base URL: %w", err)
		}
		clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
	}

	client := openai.NewClient(clientOpts...)

	// Build user prompt from input and history
	userPrompt := buildUserPrompt(params.UserInput, params.ConversationHistory)

	// Build request (same as non-streaming)
	req := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(model),
		Instructions: openai.String(params.Instructions),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
		Background: openai.Bool(true),
	}

	// Apply optional parameters
	if cfg.Temperature != nil {
		req.Temperature = openai.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		req.TopP = openai.Float(*cfg.TopP)
	}
	if cfg.MaxOutputTokens != nil {
		req.MaxOutputTokens = openai.Int(int64(*cfg.MaxOutputTokens))
	}

	// Apply reasoning effort
	if effort := cfg.ExtraOptions["reasoning_effort"]; effort != "" {
		req.Reasoning = shared.ReasoningParam{
			Effort: mapReasoningEffort(effort),
		}
	}

	// Apply service tier
	if tier := cfg.ExtraOptions["service_tier"]; tier != "" {
		req.ServiceTier = mapServiceTier(tier)
	}

	// Apply verbosity setting
	if verbosity := cfg.ExtraOptions["verbosity"]; verbosity != "" {
		textConfig := responses.ResponseTextConfigParam{}
		textConfig.SetExtraFields(map[string]any{
			"verbosity": strings.ToLower(verbosity),
		})
		req.Text = textConfig
	}

	// Apply prompt cache retention for gpt-5.x models
	if supportsPromptCacheRetention(model) {
		retention := cfg.ExtraOptions["prompt_cache_retention"]
		if retention == "" {
			retention = "24h"
		}
		req.SetExtraFields(map[string]any{
			"prompt_cache_retention": retention,
		})
	}

	// Build tools
	var tools []responses.ToolUnionParam
	if params.EnableFileSearch && strings.TrimSpace(params.FileStoreID) != "" {
		tools = append(tools, responses.ToolUnionParam{
			OfFileSearch: &responses.FileSearchToolParam{
				Type:           constant.FileSearch("file_search"),
				VectorStoreIDs: []string{params.FileStoreID},
			},
		})
	}
	if params.EnableWebSearch {
		tools = append(tools, responses.ToolUnionParam{
			OfWebSearchPreview: &responses.WebSearchToolParam{
				Type:              responses.WebSearchToolTypeWebSearchPreview,
				SearchContextSize: responses.WebSearchToolSearchContextSizeMedium,
			},
		})
	}
	if params.EnableCodeExecution {
		tools = append(tools, responses.ToolUnionParam{
			OfCodeInterpreter: &responses.ToolCodeInterpreterParam{
				Type: constant.CodeInterpreter("code_interpreter"),
				Container: responses.ToolCodeInterpreterContainerUnionParam{
					OfCodeInterpreterContainerAuto: &responses.ToolCodeInterpreterContainerCodeInterpreterContainerAutoParam{
						Type: constant.Auto("auto"),
					},
				},
			},
		})
	}
	for _, tool := range params.Tools {
		tools = append(tools, buildFunctionTool(tool))
	}
	if len(tools) > 0 {
		req.Tools = tools
	}

	// Add previous response ID for conversation continuity
	if strings.TrimSpace(params.PreviousResponseID) != "" {
		req.PreviousResponseID = openai.String(params.PreviousResponseID)
	}

	ch := make(chan provider.StreamChunk, 100)

	go func() {
		defer close(ch)
		if cancel != nil {
			defer cancel()
		}

		stream := client.Responses.NewStreaming(ctx, req)
		defer stream.Close()

		var responseID string
		var totalText strings.Builder
		var toolCalls []provider.ToolCall
		var codeExecutions []provider.CodeExecutionResult
		// Track function names by item ID (needed because done event doesn't include name)
		functionNames := make(map[string]string)

		for stream.Next() {
			event := stream.Current()

			// Handle different event types based on Type field
			switch event.Type {
			case "response.created":
				created := event.AsResponseCreated()
				if created.Response.ID != "" {
					responseID = created.Response.ID
				}

			case "response.output_item.added":
				// Track function call names when item is added
				added := event.AsResponseOutputItemAdded()
				if added.Item.Type == "function_call" {
					fc := added.Item.AsFunctionCall()
					functionNames[fc.ID] = fc.Name
				}

			case "response.output_text.delta":
				delta := event.AsResponseOutputTextDelta()
				if delta.Delta != "" {
					ch <- provider.StreamChunk{
						Type: provider.ChunkTypeText,
						Text: delta.Delta,
					}
					totalText.WriteString(delta.Delta)
				}

			case "response.function_call_arguments.done":
				fc := event.AsResponseFunctionCallArgumentsDone()
				name := functionNames[fc.ItemID] // Look up name from when item was added
				toolCall := provider.ToolCall{
					ID:        fc.ItemID,
					Name:      name,
					Arguments: fc.Arguments,
				}
				toolCalls = append(toolCalls, toolCall)
				ch <- provider.StreamChunk{
					Type:     provider.ChunkTypeToolCall,
					ToolCall: &toolCall,
				}

			case "response.code_interpreter_call_code.done":
				ciCode := event.AsResponseCodeInterpreterCallCodeDone()
				exec := provider.CodeExecutionResult{
					Code:     ciCode.Code,
					Language: "python",
				}
				codeExecutions = append(codeExecutions, exec)

			case "response.code_interpreter_call.completed":
				// Code execution completed - outputs available
				if len(codeExecutions) > 0 {
					ch <- provider.StreamChunk{
						Type:          provider.ChunkTypeCodeExecution,
						CodeExecution: &codeExecutions[len(codeExecutions)-1],
					}
				}

			case "response.completed":
				completed := event.AsResponseCompleted()
				if completed.Response.ID != "" {
					responseID = completed.Response.ID
				}

				var usage *provider.Usage
				if completed.Response.Usage.TotalTokens > 0 {
					usage = &provider.Usage{
						InputTokens:  completed.Response.Usage.InputTokens,
						OutputTokens: completed.Response.Usage.OutputTokens,
						TotalTokens:  completed.Response.Usage.TotalTokens,
					}
				}

				ch <- provider.StreamChunk{
					Type:               provider.ChunkTypeComplete,
					ResponseID:         responseID,
					Model:              model,
					Usage:              usage,
					ToolCalls:          toolCalls,
					RequiresToolOutput: len(toolCalls) > 0,
					CodeExecutions:     codeExecutions,
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- provider.StreamChunk{
				Type:      provider.ChunkTypeError,
				Error:     err,
				Retryable: isRetryableError(err),
			}
		}
	}()

	return ch, nil
}

// buildUserPrompt constructs the user prompt from input and history.
func buildUserPrompt(userInput string, history []provider.Message) string {
	var sb strings.Builder

	if len(history) > 0 {
		sb.WriteString("Previous conversation:\n\n")
		for _, msg := range history {
			role := "User"
			if msg.Role == "assistant" {
				role = "Assistant"
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n\n", role, msg.Content))
		}
		sb.WriteString("---\n\nNew message:\n\n")
	}

	sb.WriteString(strings.TrimSpace(userInput))
	return strings.TrimSpace(sb.String())
}

// waitForCompletion polls until the response is complete.
func waitForCompletion(ctx context.Context, client openai.Client, resp *responses.Response) (*responses.Response, error) {
	if resp == nil {
		return nil, errors.New("response is nil")
	}
	if resp.Status == responses.ResponseStatusCompleted || resp.ID == "" {
		return resp, nil
	}

	pollInterval := pollInitial
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		updated, err := client.Responses.Get(ctx, resp.ID, responses.ResponseGetParams{})
		if err != nil {
			slog.Warn("response poll error", "error", err)
			continue
		}

		switch updated.Status {
		case responses.ResponseStatusCompleted:
			return updated, nil
		case responses.ResponseStatusFailed, responses.ResponseStatusCancelled, responses.ResponseStatusIncomplete:
			var msg string
			if updated.Error.JSON.Message.Valid() {
				msg = updated.Error.Message
			}
			if msg == "" {
				msg = "no error message provided"
			}
			return nil, fmt.Errorf("response ended with status %s: %s", updated.Status, msg)
		}

		// Increase poll interval
		pollInterval = min(pollInterval*2, pollMax)
	}
}

// extractCitations extracts citations from the response.
func extractCitations(resp *responses.Response, fileIDToFilename map[string]string) []provider.Citation {
	var citations []provider.Citation
	if resp == nil {
		return citations
	}

	for _, item := range resp.Output {
		if item.Type == "message" {
			msg := item.AsMessage()
			if msg.ID == "" {
				continue
			}
			for _, content := range msg.Content {
				if content.Type == "output_text" {
					textBlock := content.AsOutputText()
					if !textBlock.JSON.Type.Valid() {
						continue
					}
					for _, ann := range textBlock.Annotations {
						switch ann.Type {
						case "url_citation":
							urlCitation := ann.AsURLCitation()
							citations = append(citations, provider.Citation{
								Type:       provider.CitationTypeURL,
								Provider:   "openai",
								URL:        urlCitation.URL,
								Title:      urlCitation.Title,
								StartIndex: int(urlCitation.StartIndex),
								EndIndex:   int(urlCitation.EndIndex),
							})
						case "file_citation":
							fileCitation := ann.AsFileCitation()
							filename := fileCitation.Filename
							if fn, ok := fileIDToFilename[fileCitation.FileID]; ok {
								filename = fn
							}
							citations = append(citations, provider.Citation{
								Type:       provider.CitationTypeFile,
								Provider:   "openai",
								FileID:     fileCitation.FileID,
								Filename:   filename,
								StartIndex: int(fileCitation.Index),
							})
						}
					}
				}
			}
		}
	}

	return citations
}

// stripCitationMarkers removes OpenAI's inline file citation markers.
// These appear as "fileciteturn2file0turn2file1" in GPT-5 File Search responses.
func stripCitationMarkers(text string) string {
	return citationMarkerPattern.ReplaceAllString(text, "")
}

// supportsPromptCacheRetention checks if the model supports extended prompt cache retention.
func supportsPromptCacheRetention(model string) bool {
	return strings.HasPrefix(model, "gpt-5.")
}

// mapReasoningEffort converts string to SDK enum.
func mapReasoningEffort(effort string) shared.ReasoningEffort {
	switch strings.ToLower(effort) {
	case "none":
		return shared.ReasoningEffort("none")
	case "low":
		return shared.ReasoningEffortLow
	case "medium":
		return shared.ReasoningEffortMedium
	case "high":
		return shared.ReasoningEffortHigh
	default:
		return shared.ReasoningEffortHigh
	}
}

// mapServiceTier converts string to SDK enum.
func mapServiceTier(tier string) responses.ResponseNewParamsServiceTier {
	switch strings.ToLower(tier) {
	case "default":
		return responses.ResponseNewParamsServiceTierDefault
	case "flex":
		return responses.ResponseNewParamsServiceTierFlex
	case "priority":
		return responses.ResponseNewParamsServiceTierPriority
	default:
		return responses.ResponseNewParamsServiceTierAuto
	}
}

// isRetryableError checks if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429, 500, 502, 503, 504:
			return true
		case 400, 401, 403, 404, 422:
			return false
		}

		// Check error types
		if apiErr.Type != "" {
			switch apiErr.Type {
			case "rate_limit_error", "server_error", "api_connection_error":
				return true
			case "invalid_request_error", "authentication_error", "permission_error", "not_found_error":
				return false
			}
		}
	}

	errStr := strings.ToLower(err.Error())

	// Network errors that are retryable
	networkErrors := []string{
		"connection",
		"timeout",
		"temporary",
		"no such host",
		"tls handshake",
		"eof",
	}
	for _, netErr := range networkErrors {
		if strings.Contains(errStr, netErr) {
			return true
		}
	}

	// Context errors are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return false
}

// buildFunctionTool converts a provider.Tool to an OpenAI function tool.
func buildFunctionTool(tool provider.Tool) responses.ToolUnionParam {
	// Parse the JSON schema string into a map
	var params map[string]any
	if tool.ParametersSchema != "" {
		if err := json.Unmarshal([]byte(tool.ParametersSchema), &params); err != nil {
			slog.Warn("invalid tool parameters schema", "tool", tool.Name, "error", err)
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
	} else {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}

	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Type:        constant.Function("function"),
			Name:        tool.Name,
			Description: openai.String(tool.Description),
			Parameters:  params,
			Strict:      openai.Bool(tool.Strict),
		},
	}
}

// extractToolCalls extracts function tool calls from the response.
func extractToolCalls(resp *responses.Response) []provider.ToolCall {
	var toolCalls []provider.ToolCall
	if resp == nil {
		return toolCalls
	}

	for _, item := range resp.Output {
		if item.Type == "function_call" {
			fc := item.AsFunctionCall()
			if fc.ID == "" {
				continue
			}
			toolCalls = append(toolCalls, provider.ToolCall{
				ID:        fc.ID,
				Name:      fc.Name,
				Arguments: fc.Arguments,
			})
		}
	}

	return toolCalls
}

// extractCodeExecutions extracts code interpreter results from the response.
func extractCodeExecutions(resp *responses.Response) []provider.CodeExecutionResult {
	var executions []provider.CodeExecutionResult
	if resp == nil {
		return executions
	}

	for _, item := range resp.Output {
		if item.Type == "code_interpreter_call" {
			ci := item.AsCodeInterpreterCall()
			if ci.ID == "" {
				continue
			}

			exec := provider.CodeExecutionResult{
				Code:     ci.Code,
				Language: "python", // OpenAI code interpreter uses Python
			}

			// Extract results from outputs
			for _, output := range ci.Outputs {
				switch output.Type {
				case "logs":
					exec.Stdout = output.Logs
				case "image":
					// Image outputs have a URL
					exec.Files = append(exec.Files, provider.GeneratedFile{
						Name:     "output.png",
						MIMEType: "image/png",
					})
				}
			}

			executions = append(executions, exec)
		}
	}

	return executions
}
