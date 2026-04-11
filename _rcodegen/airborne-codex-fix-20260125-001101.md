Date Created: 2026-01-25 00:10:58 +0100
Date Updated: 2026-01-28
TOTAL_SCORE: 88/100

Overview
- Quick scan of core services (file upload, provider streaming paths, RAG, auth).
- Focused on timeout enforcement and streaming resource cleanup.

Findings and fixes

*Issue 2 (OpenAI-compat stream not closed) - FIXED in v1.7.14*
*Issue 3 (Anthropic thinking stream not closed) - FIXED in v1.7.14*

1) File upload timeout is not enforced while Recv blocks
- Risk: a stalled client can keep a server goroutine alive indefinitely despite uploadTimeout, enabling resource exhaustion.
- Fix: add a small Recv wrapper that respects ctx.Done and return proper gRPC status codes for timeout/cancel.
- Status: DEFERRED - requires significant refactoring

Patch (internal/service/files.go)
```diff
diff --git a/internal/service/files.go b/internal/service/files.go
index 2c1b2f8..a08dd1a 100644
--- a/internal/service/files.go
+++ b/internal/service/files.go
@@
 // uploadTimeout is the maximum duration allowed for a file upload stream.
 const uploadTimeout = 5 * time.Minute
+
+// recvUploadMessage enforces context cancellation while waiting on stream.Recv.
+func recvUploadMessage(ctx context.Context, stream pb.FileService_UploadFileServer) (*pb.UploadFileRequest, error) {
+	type recvResult struct {
+		msg *pb.UploadFileRequest
+		err error
+	}
+
+	resultCh := make(chan recvResult, 1)
+	go func() {
+		msg, err := stream.Recv()
+		resultCh <- recvResult{msg: msg, err: err}
+	}()
+
+	select {
+	case <-ctx.Done():
+		return nil, ctx.Err()
+	case result := <-resultCh:
+		return result.msg, result.err
+	}
+}
@@
 	// First message should be metadata
-	firstMsg, err := stream.Recv()
+	firstMsg, err := recvUploadMessage(ctx, stream)
 	if err != nil {
-		return fmt.Errorf("receive metadata: %w", err)
+		if err == context.DeadlineExceeded {
+			return status.Error(codes.DeadlineExceeded, "upload timeout exceeded")
+		}
+		if err == context.Canceled {
+			return status.Error(codes.Canceled, "upload canceled")
+		}
+		return fmt.Errorf("receive metadata: %w", err)
 	}
@@
-		// Check for context cancellation (timeout)
-		select {
-		case <-ctx.Done():
-			return status.Error(codes.DeadlineExceeded, "upload timeout exceeded")
-		default:
-		}
-
-		msg, err := stream.Recv()
+		msg, err := recvUploadMessage(ctx, stream)
 		if err == io.EOF {
 			break
 		}
 		if err != nil {
-			return fmt.Errorf("receive chunk: %w", err)
+			if err == context.DeadlineExceeded {
+				return status.Error(codes.DeadlineExceeded, "upload timeout exceeded")
+			}
+			if err == context.Canceled {
+				return status.Error(codes.Canceled, "upload canceled")
+			}
+			return fmt.Errorf("receive chunk: %w", err)
 		}
```

2) OpenAI-compatible streaming responses are never closed
- Risk: response bodies/connections can leak under load.
- Fix: close the streaming response when the goroutine exits.

Patch (internal/provider/compat/openai_compat.go)
```diff
diff --git a/internal/provider/compat/openai_compat.go b/internal/provider/compat/openai_compat.go
index 0d1f0a7..f1f3e7d 100644
--- a/internal/provider/compat/openai_compat.go
+++ b/internal/provider/compat/openai_compat.go
@@
 		if cancel != nil {
 			defer cancel()
 		}
 
 		stream := client.Chat.Completions.NewStreaming(ctx, reqParams)
+		defer stream.Close()
 		var fullText strings.Builder
 		var usage *provider.Usage
```

3) Anthropic thinking-enabled GenerateReply streaming is not closed
- Risk: streaming responses remain open for thinking-enabled requests, leaking resources.
- Fix: close the stream after the accumulation loop.

Patch (internal/provider/anthropic/client.go)
```diff
diff --git a/internal/provider/anthropic/client.go b/internal/provider/anthropic/client.go
index 2efc6c3..d95b8e5 100644
--- a/internal/provider/anthropic/client.go
+++ b/internal/provider/anthropic/client.go
@@
 		// Use streaming for thinking operations (required by Anthropic for long operations)
 		if thinkingEnabled {
 			stream := client.Messages.NewStreaming(reqCtx, reqParams)
 			accumulated := anthropic.Message{}
 			for stream.Next() {
 				event := stream.Current()
 				if accErr := accumulated.Accumulate(event); accErr != nil {
 					err = fmt.Errorf("stream accumulation error: %w", accErr)
 					break
 				}
 			}
+			stream.Close()
 			if stream.Err() != nil {
 				err = stream.Err()
 			} else if err == nil {
 				resp = &accumulated
 			}
 		} else {
```

Notes
- No code edits applied; patches are provided for manual application.
- Tests not run.
- If you apply these patches, remember to bump `VERSION` and update `CHANGELOG.md` per AGENTS.md, then commit.
