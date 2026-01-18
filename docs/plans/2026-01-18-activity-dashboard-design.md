# Airborne Activity Dashboard Implementation Plan

**Date:** 2026-01-18
**Author:** Claude Code (Opus 4.5)
**Status:** Implementation

---

## Overview

Add PostgreSQL message storage and a Live Activity Feed dashboard to Airborne, enabling real-time monitoring of LLM requests across all tenants.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        AIRBORNE SERVICE                          │
├─────────────────────────────────────────────────────────────────┤
│  1. PostgreSQL Client (internal/db/postgres.go)                 │
│  2. Repository Layer (internal/db/repository.go)                │
│  3. Message Persistence (hook into chat.go)                     │
│  4. HTTP Admin Server (internal/admin/server.go)                │
│     - GET /admin/activity                                        │
│     - GET /admin/health                                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ HTTP (port 50052)
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      DASHBOARD (Next.js)                         │
├─────────────────────────────────────────────────────────────────┤
│  - Live Activity Feed table (ported from Bizops)                │
│  - 3-second polling                                              │
│  - Content detail modal                                          │
│  - Provider badges, token formatting                            │
└─────────────────────────────────────────────────────────────────┘
```

## Database Schema

### Tables

```sql
airborne_threads (
    id UUID PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    provider TEXT,
    model TEXT,
    status TEXT DEFAULT 'active',
    message_count INT DEFAULT 0,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
)

airborne_messages (
    id UUID PRIMARY KEY,
    thread_id UUID REFERENCES airborne_threads,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    provider TEXT,
    model TEXT,
    response_id TEXT,
    input_tokens INT,
    output_tokens INT,
    total_tokens INT,
    cost_usd DECIMAL(10,6),
    processing_time_ms INT,
    citations JSONB,
    created_at TIMESTAMPTZ
)
```

### Activity Feed View

```sql
CREATE VIEW airborne_activity_feed AS
SELECT m.*, t.tenant_id, t.user_id,
       (SELECT SUM(cost_usd) FROM airborne_messages WHERE thread_id = m.thread_id) AS thread_cost_usd
FROM airborne_messages m
JOIN airborne_threads t ON m.thread_id = t.id
WHERE m.role = 'assistant'
ORDER BY m.created_at DESC
LIMIT 50;
```

## Configuration

```yaml
database:
  enabled: true
  url: "${DATABASE_URL}"
  max_connections: 10
  log_queries: false

admin:
  enabled: true
  port: 50052
```

## Go Implementation

### New Packages

- `internal/db/` - PostgreSQL connection, repository, models
- `internal/admin/` - HTTP admin server
- `internal/pricing/` - Cost calculation per model

### Integration Points

1. `ChatService` gets optional `Repository` field
2. After successful LLM response, persist messages asynchronously
3. Admin server runs alongside gRPC server

## Dashboard

### Technology

- Next.js 16 + React 19
- Tailwind CSS v4
- TypeScript

### Features

- Live Activity Feed table with 3-second polling
- Pause/Resume/Clear controls
- Provider color coding (Anthropic amber, Gemini cyan, OpenAI emerald)
- Content detail modal
- Token formatting (K suffix for thousands)

## Implementation Phases

1. **Database Layer** - Config, connection pool, models, repository, migration
2. **Pricing & Persistence** - Cost calculation, chat.go integration
3. **Admin HTTP Server** - Activity endpoint
4. **Dashboard** - Next.js app with ActivityTable
5. **Testing & Documentation** - Integration tests, version bump

## Reference

Based on design document: `bizops/_studies/airborne-message-storage-design.md`
UI ported from: `bizops/admin/frontend/src/components/Airborne.jsx`
