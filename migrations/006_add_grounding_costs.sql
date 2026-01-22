-- Migration 006: Add grounding cost tracking columns
-- Google Web Search / Grounding tool costs are charged separately from token costs
-- Gemini 3: $14 / 1,000 search queries
-- Gemini 2.5 and older: $35 / 1,000 grounded prompts

-- Add to messages table (per-message tracking)
ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_queries INTEGER;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION;

-- Add to activity table (denormalized view for dashboard)
ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_queries INTEGER DEFAULT 0;
ALTER TABLE activity ADD COLUMN IF NOT EXISTS grounding_cost_usd DOUBLE PRECISION DEFAULT 0;

-- Create index for queries that filter/sort by grounding cost
CREATE INDEX IF NOT EXISTS idx_messages_grounding_cost ON messages(grounding_cost_usd) WHERE grounding_cost_usd IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_activity_grounding_cost ON activity(grounding_cost_usd) WHERE grounding_cost_usd > 0;
