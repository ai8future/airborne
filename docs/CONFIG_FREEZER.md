# Config Freezer - Production Configuration Management

## Problem

Airborne's configuration system has 40+ options loaded from multiple sources:
- YAML config files
- 25+ environment variables
- Doppler API calls (with retry logic)
- Secret resolution (ENV=, FILE=, inline)
- Multi-tenant configuration loading
- Feature flags and optional components

This complexity causes **production meltdowns**:
- Missing `DOPPLER_TOKEN` silently falls back to file loading
- Database auto-enables when `DATABASE_URL` is present
- Tenant resolution happens per-request (manager can reload)
- Auth mode incompatibilities only caught at first request
- No startup validation of critical dependencies

## Solution: Config Freezer

The **Config Freezer** pre-resolves ALL configuration at build/deploy time and creates a single frozen JSON file. Production loads this frozen config with zero logic - just JSON parsing.

### Benefits

✅ **Fail-fast validation** - Config errors caught at freeze-time, not runtime
✅ **No Doppler API calls** - All secrets pre-resolved
✅ **No environment variable parsing** - Everything is hardcoded
✅ **No feature flag checks** - Static configuration
✅ **Predictable startup** - Same config every time
✅ **Faster startup** - JSON parse vs complex loading logic

## Usage

### 1. Generate Frozen Config

Run the freeze command in your staging/dev environment where you have access to all secrets:

```bash
# Set all required environment variables
export DOPPLER_TOKEN=dp.st.xxxx
export AIRBORNE_ADMIN_TOKEN=your-token
export DATABASE_URL=postgresql://...
# ... all other env vars

# Run freeze command
go run cmd/airborne-freeze/main.go

# Output: configs/frozen.json
```

**What it does:**
1. Loads global config (triggers Doppler, env vars, etc.)
2. Loads all tenant configs (Doppler or files)
3. Resolves all `ENV=` and `FILE=` secrets
4. Validates everything (fails if any issues)
5. Writes `configs/frozen.json`

### 2. Deploy Frozen Config

Copy `frozen.json` to production and set environment variables:

```bash
export AIRBORNE_USE_FROZEN=true
export AIRBORNE_FROZEN_CONFIG_PATH=/etc/airborne/frozen.json

# Start airborne - it will use frozen config
./airborne
```

### 3. Verify Frozen Mode

Check logs at startup:

```
INFO: Loading frozen configuration path=/etc/airborne/frozen.json
```

If you see this, you're in frozen mode. No Doppler calls, no env var parsing, no complexity.

## Frozen Config Structure

```json
{
  "global_config": {
    "server": {"grpc_port": 50612, "host": "0.0.0.0"},
    "redis": {"addr": "redis:6379", "password": "actual-password", "db": 0},
    "database": {
      "enabled": true,
      "url": "postgresql://actual-connection-string",
      "max_connections": 10
    },
    "providers": {
      "openai": {
        "enabled": true,
        "default_model": "gpt-4o"
      }
    }
  },
  "tenant_configs": [
    {
      "tenant_id": "brand-a",
      "display_name": "Brand A",
      "providers": {
        "openai": {
          "enabled": true,
          "api_key": "sk-actual-key-resolved",
          "model": "gpt-4o"
        }
      }
    }
  ],
  "frozen_at": "2026-01-22T10:30:00Z",
  "single_tenant": false
}
```

**All secrets are resolved** - no `ENV=OPENAI_KEY`, just the actual key value.

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Build and Deploy

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      # Freeze config in CI with access to secrets
      - name: Generate frozen config
        env:
          DOPPLER_TOKEN: ${{ secrets.DOPPLER_TOKEN }}
          AIRBORNE_ADMIN_TOKEN: ${{ secrets.ADMIN_TOKEN }}
        run: |
          go run cmd/airborne-freeze/main.go

      # Build binary
      - name: Build
        run: go build -o airborne cmd/airborne/main.go

      # Package frozen config with binary
      - name: Package
        run: |
          mkdir -p dist/configs
          cp airborne dist/
          cp configs/frozen.json dist/configs/
          tar -czf airborne-release.tar.gz dist/

      # Deploy package
      - name: Deploy
        run: |
          # Upload to production servers
          # Servers only need AIRBORNE_USE_FROZEN=true
```

## Docker Example

```dockerfile
FROM golang:1.22 AS freezer

WORKDIR /app
COPY . .

# Install dependencies
RUN go mod download

# Generate frozen config at build time
# Secrets passed as build args
ARG DOPPLER_TOKEN
ARG ADMIN_TOKEN
ENV DOPPLER_TOKEN=${DOPPLER_TOKEN}
ENV AIRBORNE_ADMIN_TOKEN=${ADMIN_TOKEN}

RUN go run cmd/airborne-freeze/main.go

# Build stage
FROM golang:1.22 AS builder
WORKDIR /app
COPY . .
RUN go build -o airborne cmd/airborne/main.go

# Production image
FROM debian:bookworm-slim
WORKDIR /app

# Copy binary and frozen config
COPY --from=builder /app/airborne .
COPY --from=freezer /app/configs/frozen.json /etc/airborne/frozen.json

# Production only needs these env vars
ENV AIRBORNE_USE_FROZEN=true
ENV AIRBORNE_FROZEN_CONFIG_PATH=/etc/airborne/frozen.json

CMD ["./airborne"]
```

## Advanced: Custom Freeze Path

```bash
# Freeze to custom location
export AIRBORNE_FROZEN_CONFIG_PATH=/var/airborne/prod-config.json
go run cmd/airborne-freeze/main.go

# Use in production
export AIRBORNE_USE_FROZEN=true
export AIRBORNE_FROZEN_CONFIG_PATH=/var/airborne/prod-config.json
./airborne
```

## Security Notes

⚠️ **Frozen config contains secrets in plaintext**

- Store `frozen.json` with `0600` permissions
- Never commit to version control
- Use encrypted storage in CI/CD
- Rotate secrets regularly and re-freeze

### Recommended Permissions

```bash
# Set restrictive permissions
chmod 600 configs/frozen.json
chown airborne:airborne configs/frozen.json
```

## Troubleshooting

### Freeze fails with "doppler tenant load error"

**Cause:** Missing or invalid `DOPPLER_TOKEN`

**Fix:**
```bash
# Verify token is set
echo $DOPPLER_TOKEN

# Test Doppler access
curl -u "$DOPPLER_TOKEN:" https://api.doppler.com/v3/projects
```

### Freeze fails with "tenant config validation failed"

**Cause:** Invalid tenant configuration (missing API key, invalid model, etc.)

**Fix:** Check the error message for which tenant and field failed validation. Update the tenant config source (Doppler or file).

### Production still loading from env vars

**Cause:** `AIRBORNE_USE_FROZEN` not set

**Fix:**
```bash
export AIRBORNE_USE_FROZEN=true
```

Verify logs show:
```
INFO: Loading frozen configuration path=configs/frozen.json
```

### Frozen config is stale

**Cause:** Config was frozen weeks ago, secrets have rotated

**Fix:** Re-run freeze command with current secrets:
```bash
# Update secrets first
export DOPPLER_TOKEN=new-token

# Re-freeze
go run cmd/airborne-freeze/main.go

# Re-deploy frozen.json
```

## Migration Path

### Step 1: Test in Staging

```bash
# In staging environment
go run cmd/airborne-freeze/main.go
export AIRBORNE_USE_FROZEN=true
./airborne

# Verify everything works
curl http://localhost:50054/health
```

### Step 2: Integrate in CI/CD

Add freeze step to your build pipeline (see examples above).

### Step 3: Deploy to Production

1. Build with frozen config in CI
2. Deploy package with `frozen.json`
3. Set `AIRBORNE_USE_FROZEN=true` in production env
4. Start services
5. Monitor logs for "Loading frozen configuration"

### Step 4: Remove Dev Config Loading (Optional)

Once confident, you can strip out unused config code:
- Remove Doppler client code
- Remove env var parsing in `applyEnvOverrides()`
- Remove file-based tenant loading

This reduces binary size and attack surface.

## Performance Impact

| Metric | Before (Dynamic) | After (Frozen) |
|--------|------------------|----------------|
| Startup config load | ~300-500ms | ~5ms |
| Doppler API calls | 1-5 per tenant | 0 |
| Environment var parsing | 25+ vars | 0 |
| Secret resolution | Per-tenant | 0 |
| Validation | Runtime (partial) | Build-time (complete) |

**Estimated startup improvement:** 200-400ms faster

## Related Configuration Improvements

This PR also includes:

1. **Fatal validation errors** - Invalid `startup_mode` now fails instead of warning
2. **No database auto-enable** - Must explicitly set `DATABASE_ENABLED=true`
3. **Doppler fallback logging** - Clear logs when using Doppler vs files
4. **Tenant loading visibility** - Shows count of loaded tenants

## Future Enhancements

- [ ] Config diff tool (compare frozen vs live)
- [ ] Config rotation automation
- [ ] Per-environment frozen configs
- [ ] Encrypted frozen configs (decrypt at startup)
- [ ] Config version tracking in frozen.json

## Support

Questions? Issues?
- File an issue with the `config` label
- Check logs for "Loading frozen configuration"
- Verify `AIRBORNE_USE_FROZEN=true` is set
- Compare `frozen.json` structure with examples above
