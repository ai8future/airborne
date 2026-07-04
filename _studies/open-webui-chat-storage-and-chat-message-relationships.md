# Open WebUI Chat Storage & chat_message Relationships

**Date:** July 4, 2026
**Source codebase:** `open-webui` (backend/open_webui) — SQLAlchemy 2.0 + Alembic

## Summary

Open WebUI persists AI conversations using **two coexisting storage models**:

1. **Legacy / source-of-truth:** the `chat` table, which stores the *entire conversation* as a single JSON blob (a branching message tree) in one column.
2. **Newer normalized layer:** the `chat_message` table, one row per message, kept in sync via **dual-writes** from the JSON blob.

Structurally, `chat_message` hangs off `chat` by a **single database-enforced foreign key** (`chat_id`, `ON DELETE CASCADE`). All of its other "connections" — to `user`, `model`, `file`, its own `parent_id` tree, and the peripheral `chat_file` / `feedback` tables — are **application-level soft references** (indexed columns or ids embedded in JSON), not enforced FK constraints. This is consistent with a normalized layer that is still mid-migration out of the original JSON blob.

---

## 1. Database Stack

- **ORM:** SQLAlchemy 2.0.50 (asyncio) — schema is code-first in Python model classes.
- **Migrations:** Alembic 1.18.4 — 48 versioned migrations in `backend/open_webui/migrations/versions/`.
- **Base/metadata:** `backend/open_webui/internal/db.py` (`Base = declarative_base(...)`).
- **Databases supported:** PostgreSQL (`psycopg` v3 + `psycopg2`), MySQL/MariaDB (`PyMySQL`), SQLite (incl. encrypted `sqlite+sqlcipher`).
- **Models directory:** `backend/open_webui/models/` (~28 model files).

---

## 2. The `chat` table — one row = one conversation

File: `backend/open_webui/models/chats.py:44`

```python
class Chat(Base):
    __tablename__ = 'chat'
    id         = Column(String, primary_key=True)   # conversation id (uuid)
    user_id    = Column(String, index=True)          # owner
    title      = Column(Text)                         # visible title
    chat       = Column(JSON)                         # ← ENTIRE CONVERSATION as JSON
    created_at = Column(BigInteger, index=True)
    updated_at = Column(BigInteger, index=True)
    share_id   = Column(Text, unique=True)            # public share-link token
    archived   = Column(Boolean, default=False)
    pinned     = Column(Boolean, default=False)
    meta       = Column(JSON, server_default='{}')    # holds tags, etc.
    folder_id  = Column(Text, nullable=True)          # organizes into folders
    tasks      = Column(JSON)
    summary    = Column(Text)                          # context-compaction summary
    last_read_at = Column(BigInteger)
```

**Performance indexes:** `folder_id_idx`, `user_id_pinned_idx`, `user_id_archived_idx`, `updated_at_user_id_idx`, `folder_id_user_id_idx`.

### The JSON message tree

The `chat` JSON column is a message **tree**, not a flat list:

```jsonc
{
  "history": {
    "messages": {
      "<msg-id>": { "id", "parentId", "childrenIds", "role", "content", "model", ... },
      ...
    },
    "currentId": "<id of the currently-displayed leaf message>"
  },
  "models": [...], "params": {...}, ...
}
```

- Messages keyed by id in a `messages` map, linked by `parentId` / `childrenIds`.
- This tree is what enables **branching** — regenerating or editing a message forks a new branch; `currentId` points at the active leaf so the UI knows which path to render.

---

## 3. The `chat_message` table — one row = one message

File: `backend/open_webui/models/chat_messages.py:80`

```python
class ChatMessage(Base):
    __tablename__ = 'chat_message'
    id         = Column(Text, primary_key=True)  # composite: "{chat_id}-{message_id}"
    chat_id    = Column(Text, ForeignKey('chat.id', ondelete='CASCADE'), index=True)
    user_id    = Column(Text, index=True)
    role       = Column(Text)         # user / assistant / system
    parent_id  = Column(Text)         # tree structure (soft self-ref)
    content    = Column(JSON)         # str or list of content blocks
    output     = Column(JSON)
    model_id   = Column(Text, index=True)
    files      = Column(JSON)         # attachment references
    sources    = Column(JSON)         # RAG citations
    embeds     = Column(JSON)
    done       = Column(Boolean)      # streaming state
    status_history = Column(JSON)
    error      = Column(JSON)
    usage      = Column(JSON)          # token counts / timing
    context_summary = Column(Text)
    created_at = Column(BigInteger, index=True)
    updated_at = Column(BigInteger)
```

**Indexes:** `chat_message_chat_parent_idx (chat_id, parent_id)`, `chat_message_model_created_idx (model_id, created_at)`, `chat_message_user_created_idx (user_id, created_at)`.

### Dual-write architecture

The `chat` write path (`chats.py:375` in `insert_new_chat`, `:442` on update) walks `history.messages` in the JSON blob and calls `ChatMessages.upsert_message(...)` for each message. So:

- The **JSON blob remains the source of truth**.
- `chat_message` is a normalized mirror kept in sync.
- Row ids use a composite scheme `{chat_id}-{message_id}` (`chat_messages.py:174`) so message ids are unique across chats.

---

## 4. Everything that connects to `chat_message`

### The ONE hard foreign key

| Relationship | Definition | Notes |
|---|---|---|
| `chat` ← `chat_message.chat_id` | `chat_messages.py:85` — `ForeignKey('chat.id', ondelete='CASCADE')` | Backbone link. Deleting a `chat` cascades to delete all its messages. |

### Self-reference (the message tree)

| Relationship | Definition | Notes |
|---|---|---|
| `chat_message.parent_id → chat_message.id` | `chat_messages.py:90` | **Not** an enforced FK. Rebuilds the branching tree from `history.messages[*].parentId`. Indexed via `chat_message_chat_parent_idx`. |

### Soft references — indexed columns, no FK

| Relationship | Definition | Notes |
|---|---|---|
| `user` ← `chat_message.user_id` | `chat_messages.py:86` | Author. Indexed for per-user analytics. No FK. |
| `model` ← `chat_message.model_id` | `chat_messages.py:97` → `model.id` (`models.py:80`) | Which model produced an assistant message. Indexed for per-model token/usage queries. No FK. |

### Soft references — inside JSON columns

| Relationship | Definition | Notes |
|---|---|---|
| `file` ← `chat_message.files` | `chat_messages.py:100` → `file.id` (`files.py:19`) | List of file records/ids as JSON. `sources` (RAG citations) and `embeds` are similar JSON id refs. |

### Sibling tables — connected to the same *chat*, not directly to `chat_message`

| Table | File | How it connects |
|---|---|---|
| **`chat_file`** | `chats.py:100` | Hard FK to `chat.id` **and** `file.id` (both CASCADE) + nullable `message_id` (soft ref to a `chat_message`). Normalized attachment join table (chat ↔ message ↔ file). |
| **`shared_chats`** | `shared_chats.py:22` | Hard FK to `chat.id` (CASCADE). A public share is a snapshot/pointer of a whole chat; touches messages only via `chat`. |
| **`feedback`** | `feedbacks.py` | Thumbs up/down. Stores `chat_id` + `message_id` **inside its JSON `meta`/`data`** (`feedbacks.py:88`, queried via `Feedback.meta['chat_id']` at `:208`). Soft refs, no FK. |

---

## 5. Relationship diagram

```
                       user            model            file
                        ▲                ▲                ▲
              user_id   │  model_id      │       files[]  │  (JSON)
              (soft)    │  (soft)        │       (soft)   │
                        │                │                │
   chat ◀───────────── chat_message ─────────────────────┘
   (JSON  chat_id FK    │   ▲
   blob   CASCADE)      │   │ parent_id (soft self-ref = message tree)
    ▲  ▲               └───┘
    │  │
    │  └──── chat_file (FK chat.id + FK file.id; soft message_id ─▶ chat_message)
    │
    ├──────── shared_chats (FK chat.id, CASCADE)
    │
    └········ feedback (chat_id + message_id stored in JSON meta — soft)
```

---

## 6. Important distinction — do NOT confuse with `message` / `message_reaction`

File: `backend/open_webui/models/messages.py` defines a **separate** `message` table with a `channel_id`. That is for the **Channels** feature (Slack-style real-time group chat with reactions, pins, replies) — **not** the AI conversation threads. AI conversations live exclusively in `chat` / `chat_message`.

(`channels.py:163` shows the channel-side attachment join table referencing `channel.id`, `message.id`, and `file.id`.)

---

## Key Takeaways

1. **A conversation = one `chat` row**, with the full branching message tree serialized into the `chat` JSON column (`history.messages` + `currentId`).
2. **`chat_message` is a newer normalized mirror**, one row per message, populated by dual-writes; the JSON blob is still authoritative.
3. **Only one enforced FK on `chat_message`** (`chat_id → chat.id`, CASCADE). Everything else is a soft reference — a sign the relational migration is incomplete.
4. **Branching** is modeled by `parentId`/`childrenIds` (JSON) mirrored as `parent_id` (relational self-ref).
5. **Channels (`message` table) are a different feature** — keep them mentally separate from chats.
