# Gemini filestore bypassed lazy call client

The Gemini filestore path could issue upload/download requests without first resolving the shared lazy `call.Client`, bypassing the configured retry and circuit-breaker transport. That made filestore operations less resilient than normal provider calls and could silently skip the intended call-chassis protections.

Fixed by routing Gemini filestore GET and POST requests through the lazy call client and by rewinding POST bodies before retries. Regression tests cover both GET retry behavior and POST body rewind across retry attempts.
