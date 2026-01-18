# Structured Metadata Feature Design

**Date:** 2026-01-18
**Status:** Approved
**Priority:** Medium (rarely used but preserving for future use)

## Overview

Move StructuredMetadata extraction from Solstice to Airborne. When clients request structured output mode, Gemini returns JSON with extracted intent, entities, topics, and scheduling signals alongside the response text.

## Goals

1. Enable structured output via request parameter (`enable_structured_output: true`)
2. Return raw structured data in proto response (no rendering)
3. Support Gemini provider (silently ignored for other providers)
4. Maintain backward compatibility (optional feature)

## Non-Goals

- Trigger detection (`@structured` phrase) - stays in Solstice
- `FormatMarkdown()` admin view rendering - stays in Solstice
- Support for non-Gemini providers

---

## Proto Changes

### Request Field

Add to `GenerateReplyRequest`:
```protobuf
// Enable structured output mode (Gemini-only)
// When true, response includes structured_metadata with intent, entities, topics
bool enable_structured_output = 21;
```

### New Messages

```protobuf
// StructuredMetadata contains extracted metadata from structured output mode
message StructuredMetadata {
  string intent = 1;              // question, request, task_delegation, feedback, complaint, follow_up, attachment_analysis
  bool requires_user_action = 2;  // True if response asks a clarifying question
  repeated StructuredEntity entities = 3;
  repeated string topics = 4;     // 2-4 keyword tags
  SchedulingIntent scheduling = 5;
}

// StructuredEntity represents an extracted named entity
message StructuredEntity {
  string name = 1;   // Entity name as it appears in text
  string type = 2;   // person, organization, location, product, technology, tool, service, etc. (21 types)
}

// SchedulingIntent contains calendar/meeting signals
message SchedulingIntent {
  bool detected = 1;
  string datetime_mentioned = 2;  // Raw text like "next Tuesday at 2pm"
}
```

### Response Fields

Add to `GenerateReplyResponse`:
```protobuf
StructuredMetadata structured_metadata = 15;
```

Add to `StreamComplete`:
```protobuf
StructuredMetadata structured_metadata = 11;
```

---

## Go Types (`internal/provider/provider.go`)

```go
// StructuredMetadata contains extracted metadata from structured output mode
type StructuredMetadata struct {
    Intent             string             // question, request, task_delegation, feedback, complaint, follow_up, attachment_analysis
    RequiresUserAction bool               // True if response asks a clarifying question
    Entities           []StructuredEntity
    Topics             []string           // 2-4 keyword tags
    Scheduling         *SchedulingIntent
}

// StructuredEntity represents an extracted named entity
type StructuredEntity struct {
    Name string // Entity name as it appears in text
    Type string // person, organization, location, product, technology, tool, service, etc.
}

// SchedulingIntent contains calendar/meeting signals
type SchedulingIntent struct {
    Detected          bool
    DatetimeMentioned string // Raw text like "next Tuesday at 2pm"
}
```

Add to `GenerateParams`:
```go
// EnableStructuredOutput enables JSON mode with entity extraction (Gemini-only)
EnableStructuredOutput bool
```

Add to `GenerateResult`:
```go
// StructuredMetadata contains extracted intent, entities, topics (when structured output enabled)
StructuredMetadata *StructuredMetadata
```

---

## Gemini Implementation

### New File: `internal/provider/gemini/schema.go`

Defines the JSON schema for structured output:

```go
func structuredOutputSchema() *genai.Schema {
    return &genai.Schema{
        Type: genai.TypeObject,
        Properties: map[string]*genai.Schema{
            "reply": {
                Type:        genai.TypeString,
                Description: "The conversational response in Markdown format",
            },
            "intent": {
                Type: genai.TypeString,
                Enum: []string{
                    "question", "request", "task_delegation",
                    "feedback", "complaint", "follow_up", "attachment_analysis",
                },
                Description: "Primary intent classification",
            },
            "requires_user_action": {
                Type:        genai.TypeBoolean,
                Description: "True if response asks a clarifying question",
            },
            "entities": {
                Type: genai.TypeArray,
                Items: &genai.Schema{
                    Type: genai.TypeObject,
                    Properties: map[string]*genai.Schema{
                        "name": {Type: genai.TypeString},
                        "type": {
                            Type: genai.TypeString,
                            Enum: []string{
                                // Core (9)
                                "person", "organization", "location", "product",
                                "project", "document", "event", "money", "date",
                                // Business (3)
                                "investor", "advisor", "metric",
                                // Technology (3)
                                "technology", "tool", "service",
                                // Operations (3)
                                "methodology", "credential", "timeframe",
                                // Content (3)
                                "feature", "url", "email_address",
                            },
                        },
                    },
                },
            },
            "topics": {
                Type:        genai.TypeArray,
                Items:       &genai.Schema{Type: genai.TypeString},
                Description: "2-4 keyword tags",
            },
            "scheduling_intent": {
                Type: genai.TypeObject,
                Properties: map[string]*genai.Schema{
                    "detected":           {Type: genai.TypeBoolean},
                    "datetime_mentioned": {Type: genai.TypeString},
                },
            },
        },
        Required: []string{"reply", "intent"},
    }
}
```

### System Instruction Addition

When structured output is enabled, append to system prompt:
```
When responding with structured output:
- BEFORE writing your response, identify the key entities (companies, people, organizations, locations, technologies, etc.) you will discuss
- Populate the 'entities' array with these specific names and their types
- THEN write your detailed response in the 'reply' field discussing those entities
- This helps track the specific businesses, people, technologies, and places mentioned in your responses
```

### Extraction Logic (`internal/provider/gemini/client.go`)

In `GenerateReply()`, when building request config:
```go
if params.EnableStructuredOutput {
    config.ResponseMIMEType = "application/json"
    config.ResponseSchema = structuredOutputSchema()
}
```

In response handling:
```go
if params.EnableStructuredOutput {
    text, metadata := extractStructuredResponse(resp)
    result.Text = text
    result.StructuredMetadata = metadata
} else {
    result.Text = extractText(resp)
}
```

New function:
```go
func extractStructuredResponse(resp *genai.GenerateContentResponse) (string, *provider.StructuredMetadata) {
    // Get raw text from response
    rawText := extractRawText(resp)

    // Parse as JSON
    var parsed struct {
        Reply              string `json:"reply"`
        Intent             string `json:"intent"`
        RequiresUserAction bool   `json:"requires_user_action"`
        Entities           []struct {
            Name string `json:"name"`
            Type string `json:"type"`
        } `json:"entities"`
        Topics           []string `json:"topics"`
        SchedulingIntent *struct {
            Detected          bool   `json:"detected"`
            DatetimeMentioned string `json:"datetime_mentioned"`
        } `json:"scheduling_intent"`
    }

    if err := json.Unmarshal([]byte(rawText), &parsed); err != nil {
        slog.Warn("failed to parse structured response, falling back to raw text", "error", err)
        return rawText, nil
    }

    // Convert to provider types
    metadata := &provider.StructuredMetadata{
        Intent:             parsed.Intent,
        RequiresUserAction: parsed.RequiresUserAction,
        Topics:             parsed.Topics,
    }

    for _, e := range parsed.Entities {
        metadata.Entities = append(metadata.Entities, provider.StructuredEntity{
            Name: e.Name,
            Type: e.Type,
        })
    }

    if parsed.SchedulingIntent != nil {
        metadata.Scheduling = &provider.SchedulingIntent{
            Detected:          parsed.SchedulingIntent.Detected,
            DatetimeMentioned: parsed.SchedulingIntent.DatetimeMentioned,
        }
    }

    return parsed.Reply, metadata
}
```

---

## Service Integration (`internal/service/chat.go`)

### Parameter Passing

In `prepareRequest()`:
```go
params := provider.GenerateParams{
    // ... existing fields ...
    EnableStructuredOutput: req.EnableStructuredOutput,
}
```

### Response Building

In `buildResponse()`:
```go
if result.StructuredMetadata != nil {
    resp.StructuredMetadata = convertStructuredMetadata(result.StructuredMetadata)
}
```

New helper:
```go
func convertStructuredMetadata(m *provider.StructuredMetadata) *pb.StructuredMetadata {
    if m == nil {
        return nil
    }
    pm := &pb.StructuredMetadata{
        Intent:             m.Intent,
        RequiresUserAction: m.RequiresUserAction,
        Topics:             m.Topics,
    }
    for _, e := range m.Entities {
        pm.Entities = append(pm.Entities, &pb.StructuredEntity{
            Name: e.Name,
            Type: e.Type,
        })
    }
    if m.Scheduling != nil {
        pm.Scheduling = &pb.SchedulingIntent{
            Detected:          m.Scheduling.Detected,
            DatetimeMentioned: m.Scheduling.DatetimeMentioned,
        }
    }
    return pm
}
```

### Streaming

In `StreamComplete` handling, include structured metadata from accumulated result.

### Non-Gemini Providers

Return nil for `StructuredMetadata` - feature is silently ignored (no error).

---

## What Stays in Solstice

After this migration, Solstice retains:

1. **Trigger detection** - `detectStructuredTrigger()` checks for `@structured` phrase
2. **FormatMarkdown()** - Renders admin view footer with intent, entities, topics
3. **Display logic** - Decides when to show metadata (admin-only, no CC recipients)
4. **Debug store** - Persists extracted metadata for audit

Solstice changes:
- Set `enable_structured_output: true` when trigger detected
- Read `structured_metadata` from Airborne response
- Keep existing `FormatMarkdown()` rendering

---

## Implementation Order

1. Proto changes + regenerate
2. Provider types in `provider.go`
3. Gemini schema in new `schema.go`
4. Gemini extraction in `client.go`
5. Service integration in `chat.go`
6. Test with Gemini

---

## Files Summary

### Create
- `internal/provider/gemini/schema.go`

### Modify
| File | Changes |
|------|---------|
| `api/proto/airborne/v1/airborne.proto` | Request field, new messages, response fields |
| `internal/provider/provider.go` | Types + GenerateParams + GenerateResult |
| `internal/provider/gemini/client.go` | Schema config, system instruction, extraction |
| `internal/service/chat.go` | Wire param, convert function |
