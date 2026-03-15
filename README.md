# Regent Backend

Go 1.25 modular monolith powering the Regent AI executive assistant platform. Always-on architecture with per-user service bundles for 24/7 email processing, AI-powered drafting, behavior analytics, and multi-channel briefings.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.25 |
| Database | Supabase PostgreSQL + pgvector |
| Router | chi v5 |
| Auth | Supabase Auth + JWT/JWKS |
| Email | go-imap v2, Gmail API |
| AI Primary | Ollama Cloud (qwen3:4b, qwen3:8b, gemma3:12b, gpt-oss:120b) |
| AI Fallback | Google Gemini Flash |
| Cache/Queue | Redis |
| Encryption | AES-256-GCM (rotating keys) |
| Notifications | Twilio, WhatsApp, Signal, Firebase |
| Payments | Stripe |
| Observability | Prometheus + slog |

## Quick Start

```bash
# Prerequisites: Go 1.25+, PostgreSQL (Supabase), Redis

# Setup
cp .env.example .env
# Edit .env with your Supabase, Redis, and AI provider credentials

# Run
make dev              # Start with hot reload
make test             # Run tests with race detector
make lint             # golangci-lint
make build            # Build static binary
make migrate-up       # Run database migrations
make migrate-down     # Rollback last migration

# Docker
docker build -t regent-backend .
docker run -p 8080:8080 --env-file .env regent-backend
```

## Architecture

### Boot Sequence

```
1. Load config (env + .env)
2. Create pgxpool (25 max conns, health checks, pgvector registration)
3. Run migrations (if RUN_MIGRATIONS=true)
4. Create Redis client (graceful degradation if unavailable)
5. Create ServiceRegistry + orchestrator
6. Start briefing dispatcher (background)
7. Initialize AI provider (Ollama Cloud)
8. Build chi router with middleware chain
9. Start HTTP server (:8080)
10. Handle graceful shutdown (SIGINT/SIGTERM)
```

### Middleware Chain (order matters)

```
RequestID -> RealIP -> Metrics -> Logger -> Recoverer -> CORS -> RateLimit -> Timeout -> Auth -> TenantScope
```

### Always-On Service Orchestrator

The core architectural differentiator. Per-user goroutine bundles run 24/7, independent of HTTP sessions:

```
ServiceRegistry
  |-- UserServiceBundle (per active user)
  |     |-- HealthReporter (heartbeat every 30s)
  |     |-- IMAPWatcher (IDLE + 2min poll fallback)
  |     |-- CronScheduler (16 scheduled jobs)
  |     |-- AIProcessor (queue consumer)
  |     |-- BriefingEngine (notification dispatcher)
  |
  |-- AI Worker Pool (global, 20 concurrent slots)
  |-- Briefing Dispatcher (Redis stream consumer)
```

Each sub-service is wrapped in a supervisor with exponential backoff (2s base, 5min max, 10 failures = terminate).

### Cron Schedule

| Job | Interval | Purpose |
|-----|----------|---------|
| email_poll | 2 min | IMAP sync |
| ai_queue | 5 min | Process queued emails |
| token_aggregation | 1 hr | Sync token counts to PostgreSQL |
| calendar_sync | 5 min | Google/Outlook Calendar polling |
| meeting_lifecycle | 1 min | Pre-meeting briefs (30min before) |
| task_reminders | 15 min | Task reminder notifications |
| nightly_batch | 24 hr | Preference signals, RAG, behavior |
| weekly_wellness | 7 days | gpt-oss:120b wellness + synthesis |

### AI Pipeline (per email)

```
Email arrives
  |-> Stage 1: Categorize + Priority + Tone (gemma3:4b, batched)
  |-> Stage 2: Summarize (ministral-3:8b)
  |-> Stage 3: Draft Reply (gemma3:12b or gpt-oss:120b)
  |-> State persisted in email_ai_status table
  |-> Progressive visibility (frontend sees partial results)
```

**AI Memory injection** (800 token budget per call):
- User Rules (scope-filtered, 200 tokens)
- Context Briefs (keyword + vector matched, 300 tokens)
- Learned Patterns (confidence >= 70%, 300 tokens)

**Smart skip**: Newsletters, promotions, spam, updates, noreply senders skip draft generation.

**Error recovery**: Stale jobs (>10min) auto-retry. Errored jobs retry up to 3 times after 5min.

## Project Structure

```
backend/
  cmd/regent/main.go          # Entry point
  internal/
    ai/                        # AI orchestration
      cache.go                 # Redis response cache (SHA-256 key, 24h TTL)
      cached_provider.go       # Cache wrapper
      circuit.go               # Circuit breaker (3 failures -> fallback)
      gemini.go                # Google Gemini provider
      models.go                # Model routing config
      ollama.go                # Ollama Cloud provider
      provider.go              # AIProvider interface
      router.go                # Task-to-model routing
      memory/                  # AI Memory (rules, briefs, patterns)
      prompts/                 # Prompt templates + builder + versioning
      queue/                   # Job queue, worker pool, pipeline
      rag/                     # RAG retriever (pgvector top-5)
      tasks/                   # Categorize, summarize, draft engines
    api/                       # HTTP handlers (34 files)
      router.go                # Route registration
      auth_handlers.go         # Signup, login, OAuth
      email_handlers.go        # Email CRUD + summaries
      dashboard_handlers.go    # Dashboard stats + audit log
      behavior_handlers.go     # Behavior intelligence
      task_handlers.go         # Task CRUD
      billing_handlers.go      # Stripe integration
      settings_handlers.go     # Profile, notifications, AI prefs
      ...
    auth/                      # JWT/JWKS, OAuth, sessions, lockout
    behavior/                  # WLB scoring, stress, relationships, productivity
    billing/                   # Stripe webhooks, plan gating, usage metering
    briefings/                 # Multi-channel notification dispatcher
    calendar/                  # Google/Outlook Calendar sync
    config/                    # Environment config loader
    crypto/                    # AES-256-GCM encryption (rotating keys)
    database/                  # pgxpool, migrations, TenantContext
    email/                     # IMAP sync, Gmail API, MIME parsing, threading
    middleware/                # Auth, CORS, RLS, rate limit, plan gate
    models/                    # Domain structs
    observability/             # Prometheus metrics, structured logging
    orchestrator/              # ServiceRegistry, bundles, cron, supervisor
    tasks/                     # Task extraction, delegation, reminders
  migrations/                  # 60 SQL migrations (up/down pairs)
  Makefile                     # dev, test, lint, build, migrate
  Dockerfile                   # Multi-stage (golang:1.25 -> distroless/static)
  go.mod                       # 33 direct dependencies
```

## Database

**60 migrations** across 50+ tables. Key tables:

| Table | Purpose |
|-------|---------|
| tenants | Multi-tenant root (plan, RLS) |
| users | Auth, timezone, language, AI prefs |
| user_accounts | Email connections (encrypted creds) |
| emails | Core email store (87+ columns) |
| email_categories | AI categorization results |
| email_summaries | AI executive briefs |
| draft_replies | AI draft replies (status workflow) |
| email_ai_status | Pipeline state tracking |
| ai_audit_log | Every AI decision (partitioned monthly) |
| user_rules | User-defined AI instructions |
| context_briefs | Situational context (keyword + vector) |
| learned_patterns | Auto-detected behavior patterns |
| behavior_profiles | WLB score, AI understanding |
| contact_relationships | Per-contact scoring |
| tasks | Extracted action items |
| calendar_events | Google/Outlook events |

**Every table has**: `tenant_id`, RLS policy, `(tenant_id, created_at)` index.

## Multi-Tenant Safety

Compile-time enforced via `TenantContext` type:

```go
// All DB functions MUST accept TenantContext, NEVER plain context.Context
func GetEmails(ctx database.TenantContext, accountID uuid.UUID) ([]Email, error)

// Before EVERY query: SET LOCAL app.tenant_id = '<uuid>'
// This activates PostgreSQL Row-Level Security
```

## API Endpoints

**Public**: `/healthz`, `/readyz`, `/metrics`, webhooks (Gmail, Stripe, Twilio)

**Auth**: signup, login, OAuth connect (Google, Microsoft), refresh, logout

**Protected** (JWT required):
- `GET/POST /emails`, `GET /emails/{id}`, `GET /summaries`
- `GET /drafts`, `POST /drafts/{id}/approve|reject`
- `GET/POST /tasks`, `DELETE /tasks/{id}`
- `GET /dashboard/stats`, `GET /audit-log`
- `GET /analytics`, `GET /analytics/services`
- `GET/PUT /settings/profile|notification-prefs|ai-prefs`
- `GET/POST /user-rules`, `GET/POST /context-briefs`
- `GET /intelligence/overview|communication|wlb|stress|relationships|productivity`
- `GET /modules/services`, `PUT /modules/services/{id}`
- `GET/POST /billing/*`

## Billing Tiers

| Feature | Free | Attache ($97) | Privy Council ($297) | Estate ($697) |
|---------|------|---------------|---------------------|---------------|
| Email accounts | 1 | 10 | 25 | Unlimited |
| Daily tokens | 50K | 500K | 2M | Unlimited |
| AI drafts | 10/day | Unlimited | Unlimited | Unlimited |
| Draft model | gemma3:12b | gemma3:12b | gpt-oss:120b | gpt-oss:120b |
| AI Memory rules | 10 | 25 | 50 | Unlimited |
| Behavior Intel | None | Basic | Full + Wellness | Full + Coaching |

## Environment Variables

```bash
# Required
DATABASE_URL=postgresql://...
SUPABASE_URL=https://...
SUPABASE_ANON_KEY=...
SUPABASE_SERVICE_KEY=...
JWT_SECRET=...

# Optional
ENVIRONMENT=development          # development|staging|production
PORT=8080
AUTH_MODE=jwt                    # jwt|stub
RUN_MIGRATIONS=true
REDIS_URL=redis://localhost:6379
OLLAMA_CLOUD_URL=https://ollama.com/api
OLLAMA_CLOUD_API_KEY=...
GEMINI_API_KEY=...
ENCRYPTION_MASTER_KEY=...
ALLOWED_ORIGINS=http://localhost:3000
STRIPE_SECRET_KEY=...
```

## Performance Targets

| Metric | Target |
|--------|--------|
| API p95 response | < 200ms |
| Server boot | < 2s |
| All user services running | < 60s |
| New email detection | < 3 seconds |
| AI fast (qwen3:4b) | < 1s |
| AI quality (gemma3:12b) | < 3s |
| RAG retrieval | < 50ms |
| Docker image | < 20MB |
| pgxpool max conns | 25 |

## Security

- RLS on every data table
- AES-256-GCM for stored credentials
- Master key in env var, never in DB
- PII redacted from all logs
- Rate limiting on auth endpoints (5/min login, 3/min signup)
- Account lockout after 10 failed attempts
- JWT/JWKS with 1h cache
- CGO_ENABLED=0 (static binary, no libc)
- Graceful credential rotation support
