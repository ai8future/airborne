-- Migration: Add rendered_html column for markdown_svc output
-- This column stores the HTML rendered from markdown responses
-- PostgreSQL TOAST automatically compresses TEXT values >2KB

ALTER TABLE airborne_messages
    ADD COLUMN IF NOT EXISTS rendered_html TEXT;

COMMENT ON COLUMN airborne_messages.rendered_html IS 'HTML rendered from markdown response by markdown_svc (TOAST-compressed)';
