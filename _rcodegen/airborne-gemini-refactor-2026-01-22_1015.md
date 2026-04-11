Date Created: 2026-01-22_1015

# Airborne Codebase Refactoring Report

## 1. Executive Summary

This report identifies opportunities for improving code quality, reducing duplication, and enhancing maintainability in the `airborne` codebase. Key areas of focus include the `ChatService` logic in `internal/service/chat.go` and the multi-tenant database handling in `internal/db/repository.go`.

**Top Priority Recommendations:**
1.  **Refactor Multi-Tenant DB Access:** Remove hardcoded tenant lists and manual `UNION ALL` queries in the database repository.
2.  **Decompose `ChatService`:** Break down the monolithic `ChatService` into smaller, focused components (Persistence, Command Handling, Provider Selection).
3.  **Standardize Persistence Logic:** Unify message persistence logic to reduce method argument bloat and code duplication.

## 2. Detailed Findings

### A. Database Layer (`internal/db/repository.go`)

**Issue 1: Hardcoded Tenant Logic & Manual Query Construction**
The repository contains hardcoded tenant IDs (`ai8`, `email4ai`, `zztest`) and constructs queries by manually concatenating SQL for each tenant.
*   **Location:** `GetActivityFeedAllTenants`, `GetDebugDataAllTenants`, `GetThreadConversationAllTenants`, `ValidTenantIDs`.
*   **Problem:** Adding a new tenant requires modifying multiple functions and SQL strings. The `UNION ALL` query in `GetActivityFeedAllTenants` is particularly brittle and prone to errors.
*   **Recommendation:**
    *   Store valid tenants in a configuration file or a database table.
    *   Refactor `GetActivityFeedAllTenants` to either use a partitioned table approach (if supported by the schema design) or dynamically generate the query based on the active configuration rather than hardcoded strings.
    *   Replace the loop in `GetDebugDataAllTenants` with a more efficient lookup if possible, or at least drive the loop from a configuration source.

**Issue 2: Method Signature Bloat & Inconsistency**
*   **Location:** `PersistConversationTurnWithDebug` vs `CreateMessage`.
*   **Problem:** `PersistConversationTurnWithDebug` takes ~16 individual arguments. This makes the function call hard to read and prone to argument order mistakes. `CreateMessage` accepts a struct, which is cleaner, but `PersistConversationTurnWithDebug` duplicates much of the insert logic instead of reusing a shared helper.
*   **Recommendation:** Introduce a `ConversationTurn` struct that encapsulates all the data required for persistence. Refactor `PersistConversationTurnWithDebug` to accept this struct.

**Issue 3: Repetitive Table Name Resolution**
*   **Location:** `threadsTable`, `messagesTable`, etc.
*   **Problem:** Identical logic is repeated to prefix table names.
*   **Recommendation:** Centralize table name resolution logic or use a struct to hold the resolved table names for the repository instance upon initialization.

### B. Service Layer (`internal/service/chat.go`)

**Issue 1: "God Object" Tendencies in `ChatService`**
The `ChatService` handles too many distinct responsibilities:
1.  gRPC Request Validation & Mapping
2.  Provider Selection Strategy (Tenant + Request overrides)
3.  Slash Command Parsing & Execution (`/image`, etc.)
4.  RAG Retrieval & Context Injection
5.  Asynchronous Database Persistence
6.  Rate Limiting
7.  Response Rendering (HTML/Markdown)
*   **Problem:** This violates the Single Responsibility Principle, making the file large (`chat.go`) and harder to test in isolation.
*   **Recommendation:**
    *   **Extract `ConversationPersister`:** Move `persistConversation`, `persistFailedRequest`, and the async goroutine logic into a dedicated struct/interface.
    *   **Extract `SlashCommandHandler`:** Move command parsing and execution logic (currently mixed in `prepareRequest` and `GenerateReply`) to a separate handler.
    *   **Extract `ProviderSelector`:** Encapsulate the complex logic of merging tenant config, request overrides, and failover defaults into a dedicated component.

**Issue 2: Duplicated Logic in Slash Command Handling**
*   **Location:** `GenerateReply` and `GenerateReplyStream`.
*   **Problem:** Both methods check `prepared.commandResult`, handle the `/image` case (generate & return), and the `/ignore` case (return empty). This logic is copy-pasted.
*   **Recommendation:** Centralize this flow. Potentially `prepareRequest` could return a "Result" that indicates if the request is already fulfilled (e.g., by an image generation or ignore command), allowing the main methods to simply return that result immediately.

**Issue 3: Repetitive Configuration Merging**
*   **Location:** `buildProviderConfig`.
*   **Problem:** The logic to merge tenant config with request overrides is complex and handles many fields manually.
*   **Recommendation:** Use a generic configuration merging utility or a dedicated builder pattern for `ProviderConfig` to make this cleaner and less error-prone.

### C. Provider Layer (`internal/provider`)

**Issue 1: Large Interface**
*   **Location:** `internal/provider/provider.go`.
*   **Problem:** The `Provider` interface has grown large, including methods for Chat, Streaming, RAG support, Web Search support, etc.
*   **Recommendation:** Interface Segregation. Consider splitting into smaller interfaces like `ChatProvider`, `StreamProvider`, `ImageProvider` (though `ImageProvider` might already be separate in `imagegen`). This allows for more targeted implementations and mocks.

## 3. Refactoring Plan

### Phase 1: Database Cleanup (High Impact, Med Effort)
1.  Define a `TenantConfig` struct or interface to replace the hardcoded strings.
2.  Refactor `repository.go` to use this config.
3.  Create a `ConversationTurn` struct and refactor `PersistConversationTurnWithDebug`.

### Phase 2: Service Decomposition (High Impact, High Effort)
1.  Create `internal/service/persistence` package and move DB logging logic there.
2.  Create `internal/service/handling` or similar for slash command logic.
3.  Update `ChatService` to inject these new dependencies.

### Phase 3: Code Cleanup (Low Impact, Low Effort)
1.  Unify duplicate logic in `GenerateReply`/`Stream`.
2.  Clean up `buildProviderConfig`.

## 4. Conclusion
The codebase is functional but showing signs of growing pains, particularly in tenant management and service responsibility scoping. Addressing the hardcoded tenant IDs is the most critical maintenance task to prevent future errors as the system scales. Decomposing `ChatService` will significantly improve testability and readability.
