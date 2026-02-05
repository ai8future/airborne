# Field-Level Encryption with encryptedcol

**Date:** January 26, 2026

## Summary

Analysis of integrating the `encryptedcol` Go library for client-side field-level encryption in Airborne. This provides encryption at rest for sensitive user data with optional searchable blind indexes.

## Library Overview

`encryptedcol` is a Go library for client-side encrypted database columns:
- **Encryption**: XSalsa20-Poly1305 (authenticated encryption)
- **Searchable**: HMAC-based blind indexes for exact-match queries
- **Key rotation**: Multi-key support with seamless rotation
- **Compression**: Automatic Zstd compression for large values (>1KB)

## Key API Methods

### Basic Encryption
```go
// Initialize cipher
cipher, err := encryptedcol.New(
    encryptedcol.WithKey("v1", masterKey),  // 32-byte key
)
defer cipher.Close()  // Zero out key material

// Encrypt/decrypt strings
ciphertext := cipher.SealString("sensitive data")
plaintext, err := cipher.OpenString(ciphertext)

// Nullable strings
ciphertext := cipher.SealStringPtr(strPtr)  // nil -> nil
plaintext, err := cipher.OpenStringPtr(ciphertext)
```

### Searchable Encryption (Blind Indexes)
```go
// Store with blind index
sealed := cipher.SealStringIndexedNormalized(
    "Alice@Example.COM",
    encryptedcol.NormalizeEmail,
)
// sealed.Ciphertext = encrypted data
// sealed.BlindIndex = HMAC hash for searching
// sealed.KeyID      = key version

// Search by blind index
cond := cipher.SearchConditionStringNormalized(
    "email", searchValue, 1, encryptedcol.NormalizeEmail,
)
query := fmt.Sprintf("SELECT * FROM users WHERE %s", cond.SQL)
rows, _ := db.Query(query, cond.Args...)
```

### Available Normalizers
| Normalizer | Example |
|------------|---------|
| `NormalizeEmail` | `" ALICE@Example.COM "` → `"alice@example.com"` |
| `NormalizePhone` | `"(555) 123-4567"` → `"5551234567"` |
| `NormalizeTrim` | `" hello "` → `"hello"` |
| `NormalizeLower` | `"Hello"` → `"hello"` |

## Database Schema Pattern

For non-searchable encrypted fields:
```sql
{field}_encrypted BYTEA,  -- Ciphertext
key_id TEXT               -- Key version (shared per row)
```

For searchable fields (add blind index):
```sql
{field}_encrypted BYTEA,
{field}_idx BYTEA,        -- Blind index for search
key_id TEXT
```

## Key Rotation

```go
// Phase 1: Add new key as default
cipher, _ := encryptedcol.New(
    encryptedcol.WithKey("v1", oldKey),
    encryptedcol.WithKey("v2", newKey),
    encryptedcol.WithDefaultKeyID("v2"),
)

// Phase 2: Migrate existing data
if cipher.NeedsRotation(ciphertext) {
    newCiphertext, _ := cipher.RotateValue(ciphertext)
}

// Phase 3: Remove old key (after all data migrated)
cipher, _ := encryptedcol.New(
    encryptedcol.WithKey("v2", newKey),
)
```

## Airborne Integration Analysis

### Fields to Encrypt

**Messages table:**
- `content` - User messages and AI responses (critical)
- `system_prompt` - System instructions
- `raw_request_json` - Full API request payload
- `raw_response_json` - Full API response payload
- `rendered_html` - HTML rendering
- `citations` - URLs, filenames, snippets

**Files table:**
- `filename` - Original filenames

### Fields NOT to Encrypt
- `user_id` - Needed for queries without blind index complexity
- Low-entropy fields (status, role) - Would leak equality patterns
- UUIDs, token counts, costs, timestamps - Non-sensitive metadata

### Implementation Approach

1. **Dual-write migration**: Write to both plaintext and encrypted columns
2. **Prefer encrypted on read**: Fall back to plaintext during transition
3. **Backfill existing data**: Batch encrypt historical rows
4. **Drop plaintext later**: After verifying all data encrypted

### Code Changes

```go
// In db.Client
type Client struct {
    pool   *pgxpool.Pool
    cipher *encryptedcol.Cipher  // Add cipher
}

// In Repository methods
func (r *Repository) CreateMessage(ctx context.Context, msg *Message) error {
    if r.cipher != nil {
        contentEnc := r.cipher.SealString(msg.Content)
        // ... encrypt other fields
        // INSERT with both plaintext and encrypted columns
    }
}

func (r *Repository) GetMessages(ctx context.Context, threadID uuid.UUID) ([]Message, error) {
    // SELECT includes encrypted columns
    // Prefer encrypted, fall back to plaintext
    if contentEnc != nil {
        msg.Content, _ = r.cipher.OpenString(contentEnc)
    }
}
```

## Best Practices

1. **Never hardcode keys** - Use environment variables or secrets manager
2. **Call cipher.Close()** - Zeros out key material when done
3. **Only blind-index high-entropy fields** - email, username, phone (not status, role)
4. **Use normalizers consistently** - Same on write AND search
5. **Index compound** - `CREATE INDEX ON table (key_id, field_idx)`

## References

- Library: `github.com/ai8future/encryptedcol`
- Integration guide: `../encryptedcol/INTEGRATION_GUIDE.md`
