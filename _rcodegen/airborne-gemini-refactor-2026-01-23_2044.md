Date Created: 2026-01-23_2044
TOTAL_SCORE: 82/100

# Airborne Codebase Refactoring Report

## 1. Executive Summary

The Airborne codebase demonstrates a solid foundation for a production-grade Go application. It adheres to standard project layouts (`cmd`, `internal`, `pkg`) and leverages robust libraries like `pgx` for database interactions and `grpc` for service communication. The architecture correctly employs the strategy pattern for AI providers, allowing for extensibility.

However, the core business logic, particularly within `ChatService`, has accumulated significant complexity. This "God Object" anti-pattern in the service layer poses risks to maintainability and testability. Additionally, multi-tenancy implementation in the database layer relies on hardcoded queries that will become brittle as the system scales.

## 2. Architecture Overview

-   **Entry Point:** `cmd/airborne` provides a clean, well-structured entry point handling configuration, logging, and graceful shutdown.
-   **Service Layer:** Implemented in `internal/service`, primarily exposing gRPC endpoints.
-   **Provider Abstraction:** `internal/provider` defines a clear interface for AI backends (OpenAI, Gemini, etc.), promoting modularity.
-   **Persistence:** `internal/db` manages PostgreSQL connections with a tenant-aware repository pattern.
-   **Authentication:** `internal/auth` handles rate limiting via Redis, ensuring API protection.

## 3. Key Findings

### Positives
-   **Strong Conventions:** The project follows standard Go idioms and directory structures.
-   **Interface Design:** The `Provider` interface is comprehensive, supporting advanced features like streaming, tool use, and RAG.
-   **Concurrency:** Efficient use of Goroutines for non-blocking persistence and streaming responses.
-   **Security:** Rate limiting and SSL configuration for database connections are well-implemented.

### Areas for Improvement
-   **Monolithic Service Logic:** `internal/service/chat.go` is overly complex (~600+ lines), mixing concerns like validation, orchestration, failover, and persistence.
-   **Hardcoded Multi-tenancy:** `internal/db/repository.go` contains `UNION ALL` queries with hardcoded tenant IDs, which violates the Open/Closed Principle.
-   **Complex Provider Implementations:** `internal/provider/openai/client.go` has a very long `GenerateReply` method that handles too many responsibilities (retry, polling, parsing).
-   **Configuration Coupling:** Provider selection logic in `ChatService` is tightly coupled with tenant configuration checks.

## 4. Refactoring Recommendations

### Priority 1: Decompose `ChatService`
The `ChatService` in `internal/service/chat.go` violates the Single Responsibility Principle.
-   **Action:** Extract logic into dedicated sub-services or handlers.
    -   `RequestValidator`: Move input validation logic here.
    -   `ProviderSelector`: Encapsulate the logic for choosing a provider and handling failover.
    -   `ConversationPersister`: Isolate the async DB persistence logic.
    -   `RAGOrchestrator`: Manage the retrieval and context injection logic.
-   **Benefit:** Improves testability of individual components and reduces the cognitive load of modifying the chat flow.

### Priority 2: Dynamic Tenant Handling in DB
The hardcoded tenant IDs in `GetActivityFeedAllTenants` (`internal/db/repository.go`) are a maintenance bottleneck.
-   **Action:** Create a `Tenants` table or configuration source to dynamically fetch active tenants. Use a loop or a dynamic query builder to construct the multi-tenant views.
-   **Benefit:** Adding a new tenant won't require code changes in the SQL queries.

### Priority 3: Simplify Provider Logic
The `GenerateReply` method in `internal/provider/openai/client.go` is dense.
-   **Action:** Break down the method:
    -   Extract request construction (parameters, tools, config) into a `buildRequest` helper.
    -   Extract the retry/polling loop into a generic `pollWithRetry` utility.
    -   Extract response parsing (citations, tools) into `parseResponse`.
-   **Benefit:** Easier to unit test specific parts of the provider integration (e.g., verifying tool formatting without making network calls).

## 5. Code Quality & Style

-   **Variable Naming:** Generally clear and descriptive.
-   **Error Handling:** Good use of wrapping errors. However, some areas swallow errors or only log them (e.g., `persistConversation` goroutine). Consider adding a metric or alert for failed async persistence.
-   **Comments:** Code is reasonably well-commented, especially exported types.

## 6. Security & Performance

-   **SSRF Protection:** `validateCustomBaseURLs` is a good security measure.
-   **Rate Limiting:** Redis Lua scripts ensure atomic operations, which is excellent for performance and correctness.
-   **Resource Management:** Database connections are pooled. Contexts are correctly used for timeouts, though ensure `context.Background()` in async goroutines has a timeout/deadline (currently 10s, which is good).

## 7. Conclusion

Airborne is a robust application with a few architectural "growing pains." By addressing the monolithic nature of `ChatService` and making the database layer more dynamic regarding tenants, the codebase will be significantly more maintainable and ready for scaling. The score of **82/100** reflects a high-quality base that just needs some structural refinement.
