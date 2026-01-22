# Google Grounding Cost Tracking Design

**Date:** 2026-01-22
**Status:** Approved

## Overview

Add tracking for Google Web Search / Grounding tool costs, which are charged separately from token costs.

### Pricing Model

| Model Family | Billing Model | Rate |
|--------------|---------------|------|
| Gemini 3 | Per search query | $14 / 1,000 queries |
| Gemini 2.5 and older | Per grounded prompt | $35 / 1,000 prompts |

## Design

### 1. Database Schema Changes

**Files:** `internal/db/models.go`, new SQL migration

Add to `Message` struct:
```go
GroundingQueries *int     `json:"grounding_queries,omitempty"`
GroundingCostUSD *float64 `json:"grounding_cost_usd,omitempty"`
```

Add to `ActivityEntry` struct:
```go
GroundingQueries int     `json:"grounding_queries"`
GroundingCostUSD float64 `json:"grounding_cost_usd"`
```

Migration SQL:
```sql
ALTER TABLE messages ADD COLUMN grounding_queries INTEGER;
ALTER TABLE messages ADD COLUMN grounding_cost_usd DOUBLE PRECISION;
ALTER TABLE activity ADD COLUMN grounding_queries INTEGER DEFAULT 0;
ALTER TABLE activity ADD COLUMN grounding_cost_usd DOUBLE PRECISION DEFAULT 0;
```

### 2. Pricing Configuration

**File:** `configs/google_grounding_pricing.json` (new)

```json
{
  "provider": "google_grounding",
  "models": {
    "gemini-3": {
      "per_thousand_queries": 14.0
    },
    "gemini-2.5": {
      "per_thousand_prompts": 35.0
    },
    "gemini-2.0": {
      "per_thousand_prompts": 35.0
    },
    "gemini-1.5": {
      "per_thousand_prompts": 35.0
    }
  },
  "metadata": {
    "updated": "2026-01-22",
    "source": "https://ai.google.dev/gemini-api/docs/pricing"
  }
}
```

**File:** `internal/pricing/pricing.go`

Add function:
```go
func CalculateGroundingCost(model string, queryCount int) float64
```

Logic:
- Detect model family by prefix matching
- Apply $14/1000 for gemini-3, $35/1000 for older models

### 3. Extracting Grounding Queries

**File:** `internal/provider/gemini/client.go`

Add function:
```go
func extractGroundingQueryCount(resp *genai.GenerateContentResponse, model string) int
```

Logic:
- For Gemini 3: return `len(candidate.GroundingMetadata.WebSearchQueries)`
- For Gemini 2.5 and older: return 1 if grounding metadata exists, 0 otherwise

### 4. Cost Flow Integration

**File:** `internal/service/chat.go`

After Gemini API call:
1. Extract tokens (existing)
2. Extract grounding query count (new)
3. Calculate token cost (existing)
4. Calculate grounding cost (new)
5. Store both costs in Message and ActivityEntry records

**File:** `internal/provider/provider.go` (or response struct)

Add to response:
```go
GroundingQueries int
GroundingCostUSD float64
```

### 5. Frontend Display

**File:** `dashboard/src/components/ConversationPanel.tsx`

Update cost display to show breakdown when grounding cost exists:
- Total cost = token cost + grounding cost
- Show breakdown in parentheses when grounding cost > 0
- Update thread totals to sum both cost fields

## Files to Modify

1. `internal/db/models.go` - Add struct fields
2. `migrations/NNNN_add_grounding_costs.sql` - Schema migration
3. `configs/google_grounding_pricing.json` - New pricing config
4. `internal/pricing/pricing.go` - Add grounding cost calculation
5. `internal/provider/gemini/client.go` - Extract query count
6. `internal/provider/provider.go` - Add response fields
7. `internal/service/chat.go` - Integrate cost calculation
8. `dashboard/src/components/ConversationPanel.tsx` - Display costs
