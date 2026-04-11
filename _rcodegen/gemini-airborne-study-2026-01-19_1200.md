Date Created: 2026-01-19 12:00:00

# Airborne Codebase Study

## 1. Executive Summary

**Airborne** is a production-grade, multi-tenant AI Gateway and Orchestration service written in Go. Its primary purpose is to unify access to various Large Language Models (LLMs) like OpenAI, Gemini, and Anthropic behind a consistent gRPC interface. Beyond simple proxying, it provides sophisticated capabilities including Retrieval-Augmented Generation (RAG), strict multi-tenant data isolation, detailed cost tracking, and administrative observability.

It is designed as a "backend-for-frontend" or robust middleware layer that handles the complexities of AI integration (retries, rate limiting, context management, tool calling) so that consuming applications don't have to.

## 2. Architecture Overview

The system follows a clean, layered architecture typical of modern Go microservices, emphasizing interface-based abstraction and dependency injection.

### 2.1 High-Level Diagram

```
[Clients] (gRPC/HTTP)
    |
    v
[Airborne Gateway]
    +-> [Auth Layer] (Interceptors, Redis Rate Limit)
    |
    +-> [Service Layer] (Chat, File, Admin)
         |
         +-> [Provider Abstraction] -> (OpenAI / Gemini / Anthropic APIs)
         |
         +-> [RAG Pipeline]
         |    +-> [Extract: Docbox]
         |    +-> [Embed: Ollama]
         |    +-> [Store: Qdrant]
         |
         +-> [Persistence] -> (Postgres)
         |
         +-> [Pricing Engine] -> (JSON Configs)
         |
         +-> [Markdown SVC] -> (External gRPC)
```

### 2.2 Tech Stack

*   **Language:** Go 1.25.5
*   **Transport:** gRPC (Protobuf v3)
*   **Database:** PostgreSQL (with `pgx` driver)
*   **Caching/Rate Limiting:** Redis
*   **Vector Search:** Qdrant
*   **Embeddings:** Ollama (Nomic Embed Text)
*   **Text Extraction:** Docbox (Pandoc-based service)
*   **Frontend:** Next.js 16 (React 19) + Tailwind CSS 4 (for Admin Dashboard)

## 3. Core Subsystems Analysis

### 3.1 Unified Provider Interface (`internal/provider`)

The heart of Airborne is the `Provider` interface (`internal/provider/provider.go`). It forces all LLM providers to conform to a strict contract, abstracting away their API differences.

*   **Capabilities:** It supports standard text generation, streaming (`GenerateReplyStream`), RAG file search (`SupportsFileSearch`), and Web Search.
*   **Normalization:**
    *   **Tools:** It maps generic `Tool` definitions to provider-specific formats (e.g., OpenAI Functions).
    *   **Citations:** It normalizes citation formats (URL vs. File) into a standard struct, handling provider idiosyncrasies (like stripping OpenAI's `fileciteturn...` markers).
    *   **Costing:** Usage metrics (input/output tokens) are standardized.
*   **Robustness:** The OpenAI implementation (`internal/provider/openai/client.go`) includes advanced handling for "Service Tiers" (scale/latency trade-offs), "Reasoning Effort" (for o1/o3 models), and aggressive retry logic with exponential backoff.

### 3.2 Retrieval-Augmented Generation (RAG) (`internal/rag`)

Airborne implements a full-stack RAG pipeline, not relying on external "AI Agents" to do the heavy lifting.

*   **Pipeline:** `Ingest` -> `Extract` -> `Chunk` -> `Embed` -> `Store`.
*   **Ingestion:** Files are processed by `Docbox` to extract clean text from various formats (PDF, Docx, etc.).
*   **Chunking:** Configurable overlap and size settings (`internal/rag/chunker`).
*   **Vector Store:** Uses Qdrant. Crucially, it manages collections dynamically based on tenancy (`{tenantID}_{storeID}`), ensuring strict data separation at the vector level.
*   **Hybrid Search:** The `Retrieve` method supports filtering by `ThreadID`, allowing for conversation-specific memory retrieval.

### 3.3 Multi-Tenancy & Data Isolation (`migrations/`)

A standout architectural decision is the approach to multi-tenancy.

*   **Schema Evolution:** Migration 001 started with a simple `tenant_id` column. Migration 004 (`004_tenant_tables.sql`) aggressively refactored this into **tenant-prefixed tables** (e.g., `ai8_airborne_messages`, `email4ai_airborne_messages`).
*   **Motivation:** The migration notes explicitly state the goal: "Replace single multi-tenant tables with per-tenant table sets... provides table-level isolation instead of row-level tenant_id filtering." This optimizes performance for high-volume tenants and strictly prevents data leakage.
*   **Unified View:** To maintain observability, a `UNION ALL` view (`airborne_tenant_activity_feed`) aggregates data for the admin dashboard without compromising the underlying storage isolation.

### 3.4 Pricing & Cost Tracking (`internal/pricing`)

Airborne treats cost as a first-class citizen.

*   **Engine:** The `Pricer` struct loads pricing definitions from JSON files (`configs/*_pricing.json`).
*   **Granularity:** It tracks `InputPerMillion` and `OutputPerMillion` for every model.
*   **Calculation:** Every message stored in Postgres includes a calculated `cost_usd` field (microsecond precision), ensuring that cost data is immutable and historical (price changes don't affect past records).

## 4. External Interactions & Dependencies

### 4.1 Internal Services
*   **Markdown Service:** `internal/markdownsvc` wraps a gRPC client to an external service dedicated to rendering Markdown to HTML (Sanitization, Mermaid diagrams, LaTeX). This offloads CPU-intensive rendering and ensures security (XSS prevention) is handled centrally.

### 4.2 Infrastructure
*   **Redis:** Used primarily for **Rate Limiting** (Requests/Min, Tokens/Min) and API Key storage. The `internal/redis` wrapper provides high-level primitives (`Incr`, `Expire`, `Scan`).
*   **Postgres:** The source of truth for conversation history and audit logs.
*   **Qdrant:** The semantic memory bank for RAG.

## 5. Observations & Motivations

### 5.1 "Build vs. Buy" Rationale
The code suggests a "Build" preference for core infrastructure. Instead of using a framework like LangChain (which is Python-centric), Airborne implements its own RAG pipeline and Provider abstractions in Go. This allows for tighter control over concurrency, memory usage, and the specific gRPC contracts required by the wider system.

### 5.2 Strict Typing & Contracts
The use of gRPC/Protobuf ensures that the interface between Airborne and its clients is strongly typed. This prevents the "loose JSON" problems often seen in REST-based AI wrappers.

### 5.3 Debugging & Auditability
The schema includes fields for `raw_request_json` and `raw_response_json` (Migration 002). This indicates a strong need for debugging "black box" AI behavior. The system doesn't just trust the LLM; it records exactly what was sent and received for compliance and debugging.

## 6. Conclusion

Airborne is a mature implementation of the "AI Gateway" pattern. It successfully addresses the "Day 2" problems of AI integration: cost control, multi-tenant security, and provider independence. The decision to move to tenant-specific database tables marks a transition from a prototype to a scalable platform ready for enterprise workloads.
