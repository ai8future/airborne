# Static auth ignored configured Redis

The gRPC server created its Redis client only inside the Redis-auth branch. The
deterministic E2E uses static auth and a stable request ID, so Linux CI reached
the server but failed the keyed idempotency gate before provider dispatch even
when a Redis address was configured.

Redis initialization now follows `redis.addr` independently from auth mode.
Static auth uses reachable Redis for keyed idempotency and deliberately degrades
to unkeyed-only startup when Redis is missing or unavailable; keyed requests
remain fail-closed. Redis auth remains startup-fatal without Redis. Server close
and gRPC/admin readiness now cover the initialized client, and the E2E topology
uses a digest-pinned ephemeral Redis plus a no-redispatch keyed replay check.

Verified with focused race tests, fast/full Go tests and vet, Docker-backed DB
integration, enforced coverage, and the complete deterministic Docker E2E.
