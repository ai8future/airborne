# Keyed GenerateReply idempotency failed open

Keyed `GenerateReply` calls could dispatch to a provider without usable Redis state, and a
post-generation cache failure still returned success after releasing the in-flight marker. That
allowed automatic retries to regenerate a response whose first provider call may already have
completed.

The keyed path now fails closed on all pre-dispatch idempotency uncertainty, returns typed gRPC
`ErrorInfo` reasons for safe pre-dispatch failures versus post-dispatch completion ambiguity, and
keeps the in-flight marker when completion storage fails. Completed retention is configurable with
a validated 48-hour minimum, and the marker lease exceeds an enforced generation ceiling.

The Task-0 review also found that delimiter-joined tenant/key Redis names could collide and that
unconditional completion/release writes had no marker ownership proof. The namespace is now a
bounded versioned hash over length-prefixed components, keys are validated as 1–255 bytes of
visible ASCII before Redis/provider dispatch, and each acquisition carries a cryptographically
random owner token. Redis Lua compare-and-complete/release operations prevent stale owners from
modifying replacement markers; loss of completion ownership returns
`idempotency_completion_ambiguous` and withholds success.
