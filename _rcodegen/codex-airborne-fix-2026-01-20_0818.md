Date Created: 2026-01-20 08:18:38 +0100
TOTAL_SCORE: 86/100

Overview
- Scope: quick static scan of core backend providers, RAG stack, and file handling.
- Time budget: limited by request; this is not an exhaustive audit.
- Tests: not run.

Score Rationale
- Solid structure and validation in most paths, but a few correctness edge cases remain.
- The most impactful issue is conversation history truncation for Gemini, which can drop recent context and degrade answers.

Findings
1) Medium: Gemini history truncation keeps the oldest messages, dropping recent context.
   - Impact: when conversation history grows beyond maxHistoryChars, the model loses the most relevant context and can respond incorrectly.
   - Location: internal/provider/gemini/client.go (buildContents).
   - Fix: iterate history from newest to oldest, then reverse to preserve chronological order.

2) Medium: Qdrant search silently returns empty results on unexpected response shapes.
   - Impact: upstream code treats it as success and may proceed with empty context, masking backend regressions or API changes.
   - Location: internal/rag/vectorstore/qdrant.go (Search).
   - Fix: return a descriptive error when response format is unexpected.

3) Low: OpenAI vector store failure messages can be blank or misleading.
   - Impact: error diagnostics degrade; ops may lose the actual failure reason when LastError is present but Code is empty.
   - Location: internal/provider/openai/filestore.go (waitForFileProcessing).
   - Fix: use OpenAI SDK JSON presence indicators to select Message/Code safely.

Patch-Ready Diffs (not applied)

1) Preserve newest conversation history for Gemini
```diff
--- a/internal/provider/gemini/client.go
+++ b/internal/provider/gemini/client.go
@@
 func buildContents(userInput string, history []provider.Message, inlineImages []provider.InlineImage) []*genai.Content {
 	var contents []*genai.Content
 
-	// Add conversation history with size limit
-	totalChars := 0
-	for _, msg := range history {
+	// Add conversation history with size limit, favoring most recent messages
+	totalChars := 0
+	for i := len(history) - 1; i >= 0; i-- {
+		msg := history[i]
 		trimmed := strings.TrimSpace(msg.Content)
 		if trimmed == "" {
 			continue
 		}
 		msgLen := len(trimmed)
 		if totalChars+msgLen > maxHistoryChars {
 			slog.Debug("truncating conversation history",
 				"total_chars", totalChars,
 				"max_chars", maxHistoryChars)
 			break
 		}
 		totalChars += msgLen
@@
 		}
 		contents = append(contents, genai.NewContentFromText(trimmed, role))
 	}
+
+	for i, j := 0, len(contents)-1; i < j; i, j = i+1, j-1 {
+		contents[i], contents[j] = contents[j], contents[i]
+	}
@@
 	contents = append(contents, &genai.Content{
 		Role:  genai.RoleUser,
 		Parts: parts,
 	})
```

2) Surface unexpected Qdrant search responses
```diff
--- a/internal/rag/vectorstore/qdrant.go
+++ b/internal/rag/vectorstore/qdrant.go
@@
 	resp, err := s.doRequest(ctx, http.MethodPost, "/collections/"+params.Collection+"/points/search", body)
 	if err != nil {
 		return nil, err
 	}
 
 	resultsRaw, ok := resp["result"].([]any)
 	if !ok {
-		return nil, nil
+		return nil, fmt.Errorf("unexpected search response format: %T", resp["result"])
 	}
```

3) Improve OpenAI vector store failure diagnostics
```diff
--- a/internal/provider/openai/filestore.go
+++ b/internal/provider/openai/filestore.go
@@
 		case openai.VectorStoreFileStatusFailed:
 			errMsg := "unknown error"
-			if vsFile.LastError.Code != "" {
-				errMsg = vsFile.LastError.Message
-			}
+			if vsFile.LastError.JSON.Message.Valid() && vsFile.LastError.Message != "" {
+				errMsg = vsFile.LastError.Message
+			} else if vsFile.LastError.JSON.Code.Valid() && vsFile.LastError.Code != "" {
+				errMsg = vsFile.LastError.Code
+			}
 			return "failed", fmt.Errorf("file processing failed: %s", errMsg)
```

Notes
- Code was not modified in the working tree to honor the "DO NOT EDIT CODE" instruction.
- Version/CHANGELOG updates and auto-commit were intentionally skipped because no code changes were applied.
