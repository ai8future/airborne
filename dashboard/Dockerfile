# Airborne Dashboard Dockerfile
# Multi-stage build for minimal production image

# Build stage
FROM node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies from the lockfile for reproducible builds
RUN npm ci

# Copy source
COPY . .

# Keep the runner-stage public asset copy valid even when the dashboard has no
# committed public assets.
RUN mkdir -p public

# Build the Next.js app
RUN npm run build

# Production stage
FROM node:22-alpine@sha256:16e22a550f3863206a3f701448c45f7912c6896a62de43add43bb9c86130c3e2 AS runner

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

# Create non-root user
RUN addgroup --system --gid 1001 nodejs && \
    adduser --system --uid 1001 nextjs

# Copy built assets from builder
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs

EXPOSE 4848

ENV PORT=4848
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
