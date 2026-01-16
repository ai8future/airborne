# Airborne Bug and Code Smell Analysis Report
Date Created: 2026-01-16 13:12:59 +0100

## Scope and Validation
- Scanned core auth, service, provider, RAG, and validation code paths for correctness and safety issues.
- Tests executed: ?   	github.com/ai8future/airborne/cmd/airborne	[no test files]
?   	github.com/ai8future/airborne/gen/go/airborne/v1	[no test files]
ok  	github.com/ai8future/airborne/internal/auth	(cached)
ok  	github.com/ai8future/airborne/internal/config	(cached)
ok  	github.com/ai8future/airborne/internal/errors	(cached)
?   	github.com/ai8future/airborne/internal/httpcapture	[no test files]
?   	github.com/ai8future/airborne/internal/provider	[no test files]
ok  	github.com/ai8future/airborne/internal/provider/anthropic	(cached)
?   	github.com/ai8future/airborne/internal/provider/cerebras	[no test files]
?   	github.com/ai8future/airborne/internal/provider/cohere	[no test files]
?   	github.com/ai8future/airborne/internal/provider/compat	[no test files]
?   	github.com/ai8future/airborne/internal/provider/deepinfra	[no test files]
?   	github.com/ai8future/airborne/internal/provider/deepseek	[no test files]
?   	github.com/ai8future/airborne/internal/provider/fireworks	[no test files]
ok  	github.com/ai8future/airborne/internal/provider/gemini	(cached)
?   	github.com/ai8future/airborne/internal/provider/grok	[no test files]
?   	github.com/ai8future/airborne/internal/provider/hyperbolic	[no test files]
?   	github.com/ai8future/airborne/internal/provider/mistral	[no test files]
?   	github.com/ai8future/airborne/internal/provider/nebius	[no test files]
ok  	github.com/ai8future/airborne/internal/provider/openai	(cached)
?   	github.com/ai8future/airborne/internal/provider/openrouter	[no test files]
?   	github.com/ai8future/airborne/internal/provider/perplexity	[no test files]
?   	github.com/ai8future/airborne/internal/provider/together	[no test files]
?   	github.com/ai8future/airborne/internal/provider/upstage	[no test files]
ok  	github.com/ai8future/airborne/internal/rag	(cached)
ok  	github.com/ai8future/airborne/internal/rag/chunker	(cached)
ok  	github.com/ai8future/airborne/internal/rag/embedder	(cached)
ok  	github.com/ai8future/airborne/internal/rag/extractor	(cached)
?   	github.com/ai8future/airborne/internal/rag/testutil	[no test files]
ok  	github.com/ai8future/airborne/internal/rag/vectorstore	(cached)
?   	github.com/ai8future/airborne/internal/redis	[no test files]
ok  	github.com/ai8future/airborne/internal/server	(cached)
ok  	github.com/ai8future/airborne/internal/service	(cached)
ok  	github.com/ai8future/airborne/internal/tenant	(cached)
ok  	github.com/ai8future/airborne/internal/validation	(cached), .

## Findings and Fixes

### 1) High: Tenant provider config mutation leaks across requests
- Location: 
- Issue:  assigns  directly to the request config and then merges request overrides into it. Because maps are reference types, this mutates the shared tenant config map. This causes cross-request contamination and potential data races under concurrent traffic.
- Fix: Deep-copy the tenant  map before applying request overrides.

Patch-ready diff:


### 2) Medium: Gemini file upload fallback discards file data and reads entire file into memory
- Location: 
- Issue:  reads the entire file into memory, then attempts a “metadata fallback” that posts JSON metadata without the file body. This can lead to silent failures (operation created without data) and unnecessary memory pressure for large files.
- Fix: Stream the file content directly in the upload request and return a clear error on non-200 responses. This removes the invalid metadata-only fallback and avoids  for the main request.

Patch-ready diff:


## Notes
- No code was modified in the working tree; changes above are provided as patch-ready diffs per request.
- If you want these applied, I can apply the patches, update VERSION/CHANGELOG.md, and auto-commit as required by AGENTS.md.
