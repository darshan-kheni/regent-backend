package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
	"github.com/darshan-kheni/regent/internal/auth"
	"github.com/darshan-kheni/regent/internal/behavior"
	"github.com/darshan-kheni/regent/internal/billing"
	"github.com/darshan-kheni/regent/internal/briefings"
	"github.com/darshan-kheni/regent/internal/config"
	"github.com/darshan-kheni/regent/internal/crypto"
	"github.com/darshan-kheni/regent/internal/email/send"
	mw "github.com/darshan-kheni/regent/internal/middleware"
	"github.com/darshan-kheni/regent/internal/observability"
	"github.com/darshan-kheni/regent/internal/orchestrator"
)

// NewRouter creates the chi router with the full middleware chain.
func NewRouter(cfg *config.Config, pool *pgxpool.Pool, registry *orchestrator.ServiceRegistry, redisClient *redis.Client, aiProvider ai.AIProvider) (http.Handler, error) {
	r := chi.NewRouter()

	// Global middleware (order matters)
	r.Use(mw.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(observability.MetricsMiddleware)
	r.Use(mw.NewLogger())
	r.Use(mw.NewRecoverer())
	r.Use(mw.NewCORS(cfg.CORS.AllowedOrigins))
	r.Use(mw.NewRateLimiter(cfg.RateLimit.RequestsPerMinute))
	r.Use(chiMiddleware.Timeout(30 * time.Second))

	// Public routes (no auth)
	r.Get("/healthz", HealthHandler())
	r.Get("/readyz", ReadyHandler(pool))
	// TODO: In production, restrict /metrics access via IP allowlist or basic auth
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	// Auth middleware
	authMW, err := mw.NewAuth(cfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("create auth middleware: %w", err)
	}

	// Auth rate limiters
	loginRL := mw.NewAuthRateLimiter(5, time.Minute)
	signupRL := mw.NewAuthRateLimiter(3, time.Minute)

	// Initialize auth services (only if Supabase is configured)
	var authHandlers *AuthHandlers
	slog.Info("auth config check",
		"supabase_url_set", cfg.Auth.SupabaseURL != "",
		"service_key_set", cfg.Auth.SupabaseServiceKey != "",
		"encryption_key_set", cfg.Auth.EncryptionMasterKey != "",
		"auth_mode", cfg.Auth.Mode,
	)
	if cfg.Auth.SupabaseURL != "" && cfg.Auth.SupabaseServiceKey != "" {
		authCfg := auth.NewConfig(cfg.Auth.SupabaseURL, cfg.Auth.SupabaseAnonKey, cfg.Auth.SupabaseServiceKey)
		supabaseClient := auth.NewSupabaseClient(authCfg)
		sessions := auth.NewSessionService(pool, supabaseClient)
		lockout := auth.NewLockoutService(pool)
		audit := auth.NewAuditLogger(pool)

		var tokenStore *auth.OAuthTokenStore
		if cfg.Auth.EncryptionMasterKey != "" {
			enc, encErr := crypto.NewEncryptor(cfg.Auth.EncryptionMasterKey)
			if encErr != nil {
				return nil, fmt.Errorf("create encryptor: %w", encErr)
			}
			tokenStore = auth.NewOAuthTokenStore(pool, enc)
		}

		authHandlers = NewAuthHandlers(supabaseClient, sessions, lockout, audit, tokenStore)
	}

	// OAuth popup flow (no auth — the popup doesn't carry the JWT)
	oauthPopupH := NewOAuthPopupHandlers(cfg)
	r.Get("/api/v1/oauth/start", oauthPopupH.HandleOAuthStart)
	r.Get("/api/v1/oauth/callback", oauthPopupH.HandleOAuthCallback)

	// Gmail push notification webhook (no auth — Google sends these)
	r.Post("/api/v1/webhooks/gmail", func(w http.ResponseWriter, r *http.Request) {
		// Placeholder — will be wired to gmail.PushHandler in full integration
		WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Delivery tracker for notification webhooks
	tracker := briefings.NewDeliveryTracker(pool)

	// WhatsApp delivery status webhook (no auth — Meta sends these)
	r.Post("/api/v1/webhooks/whatsapp/status", NewWhatsAppWebhookHandler(tracker))

	// Twilio SMS status webhook (no auth — Twilio sends these)
	r.Post("/api/v1/webhooks/twilio/status", NewTwilioWebhookHandler(tracker))

	// Stripe webhook (no auth — Stripe sends these, signature-verified in handler)
	if cfg.Billing.StripeWebhookSecret != "" {
		webhookHandler := billing.NewWebhookHandler(pool, redisClient, cfg.Billing.StripeWebhookSecret)
		RegisterWebhookRoutes(r, webhookHandler)
	}

	// Initialize Stripe billing
	billingCfg := billing.BillingConfig{
		StripeSecretKey:      cfg.Billing.StripeSecretKey,
		StripePublishableKey: cfg.Billing.StripePublishableKey,
		StripeWebhookSecret:  cfg.Billing.StripeWebhookSecret,
		StripeMode:           cfg.Billing.StripeMode,
		StripePriceFree:      cfg.Billing.StripePriceFree,
		StripePriceAttache:   cfg.Billing.StripePriceAttache,
		StripePricePrivy:     cfg.Billing.StripePricePrivy,
		StripePriceEstate:    cfg.Billing.StripePriceEstate,
		FrontendURL:          cfg.Billing.FrontendURL,
	}
	if cfg.Billing.StripeSecretKey != "" {
		if err := billing.InitStripe(billingCfg); err != nil {
			return nil, fmt.Errorf("init stripe: %w", err)
		}
	}
	billing.InitPlans(billingCfg)

	// Suppress unused variable warning for registry in case it is nil.
	_ = registry

	// Public auth routes (no JWT required)
	if authHandlers != nil {
		r.Route("/api/v1/auth", func(r chi.Router) {
			// Public (no auth)
			r.With(signupRL.Handler).Post("/signup", authHandlers.Signup)
			r.With(loginRL.Handler).Post("/login", authHandlers.Login)
			r.Post("/reset-password", authHandlers.ResetPassword)
			r.Post("/callback", authHandlers.OAuthCallback)

			// Protected (require JWT) — mounted here to avoid chi route conflict
			r.Group(func(r chi.Router) {
				r.Use(authMW)
				r.Use(mw.NewTenantScope())
				r.Post("/logout", authHandlers.Logout)
				r.Post("/refresh", authHandlers.Refresh)
				r.Get("/sessions", authHandlers.ListSessions)
				r.Delete("/sessions", authHandlers.RevokeAllSessions)
				r.Delete("/sessions/{id}", authHandlers.RevokeSession)
				r.Post("/update-password", authHandlers.UpdatePassword)
				r.Post("/connect/google", authHandlers.ConnectGoogle)
				r.Post("/connect/microsoft", authHandlers.ConnectMicrosoft)
				r.Post("/connect/google-calendar", authHandlers.ConnectGoogleCalendar)
				r.Post("/connect/microsoft-calendar", authHandlers.ConnectMicrosoftCalendar)
				r.Delete("/connect/{provider}", authHandlers.DisconnectProvider)
			})
		})
	}

	// Protected routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(authMW)
		r.Use(mw.NewTenantScope())

		// Auth management routes are registered in the public auth group above
		// with their own auth middleware to avoid chi route conflicts.

		r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
			WriteJSON(w, r, http.StatusOK, map[string]string{"message": "pong"})
		})

		// Dashboard & audit log
		dashH := NewDashboardHandlers(pool)
		r.Get("/dashboard/stats", dashH.HandleDashboardStats)
		r.Get("/audit-log", dashH.HandleAuditLog)
		r.Get("/briefings/latest-digest", dashH.HandleLatestDigest)

		// Emails
		emailH := NewEmailHandlers(pool)
		r.Get("/emails", emailH.HandleListEmails)
		r.Get("/emails/{id}", emailH.HandleGetEmail)
		r.Get("/emails/{id}/draft", emailH.HandleGetDraft)
		r.Get("/summaries", emailH.HandleListSummaries)

		// Send service (shared by compose + draft approve)
		var sendEncryptor *crypto.RotatingEncryptor
		if cfg.Auth.EncryptionMasterKey != "" {
			var encErr error
			sendEncryptor, encErr = crypto.NewRotatingEncryptor(cfg.Auth.EncryptionMasterKey, "")
			if encErr != nil {
				slog.Error("create send encryptor", "error", encErr)
			}
		}
		sender := send.NewService(pool, sendEncryptor)
		sendH := NewSendHandlers(pool, sender)

		// Draft replies
		draftH := NewDraftHandlers(pool)
		r.Get("/drafts", draftH.HandleListDrafts)
		r.Post("/drafts/{id}/approve", sendH.HandleApproveDraftAndSend)
		r.Post("/drafts/{id}/reject", draftH.HandleRejectDraft)
		r.Put("/drafts/{id}", draftH.HandleUpdateDraft)
		r.Post("/drafts/{id}/refine", draftH.HandleRefineDraft)

		// Sent emails
		sentH := NewSentHandlers(pool)
		r.Get("/sent", sentH.HandleListSent)
		r.Get("/sent/{id}", sentH.HandleGetSentEmail)
		r.Get("/sent/{id}/adjacent", sentH.HandleGetAdjacentSent)

		// Settings
		settingsH := NewSettingsHandlers(pool)
		r.Route("/settings", func(r chi.Router) {
			r.Get("/profile", settingsH.HandleGetProfile)
			r.Put("/profile", settingsH.HandleUpdateProfile)
			r.Get("/notification-prefs", settingsH.HandleGetNotificationPrefs)
			r.Put("/notification-prefs", settingsH.HandleUpdateNotificationPrefs)
			r.Get("/ai-prefs", settingsH.HandleGetAiPrefs)
			r.Put("/ai-prefs", settingsH.HandleUpdateAiPrefs)
		})

		// Notifications
		notifH := NewNotificationHandlers(pool)
		r.Get("/notifications", notifH.HandleListNotifications)

		// AI Pipeline status (live widget)
		pipelineH := NewPipelineHandlers(pool, redisClient)
		r.Get("/pipeline/status", pipelineH.HandlePipelineStatus)

		// Analytics
		analyticsH := NewAnalyticsHandlers(pool)
		r.Get("/analytics", analyticsH.HandleAnalytics)
		r.Get("/analytics/services", analyticsH.HandleAnalyticsServices)
		r.Get("/analytics/usage", analyticsH.HandleAnalyticsUsage)
		r.Get("/analytics/memory-health", analyticsH.HandleMemoryHealth)

		// Compose
		composeH := NewComposeHandlers(aiProvider)
		r.Post("/compose/send", sendH.HandleComposeSend)
		r.Post("/compose/ai-draft", composeH.HandleAiDraft)

		// AI Memory
		memoryH := NewMemoryHandlers(pool)
		r.Get("/user-rules", memoryH.HandleListUserRules)
		r.Post("/user-rules", memoryH.HandleCreateUserRule)
		r.Put("/user-rules/{id}", memoryH.HandleUpdateUserRule)
		r.Delete("/user-rules/{id}", memoryH.HandleDeleteUserRule)
		r.Get("/context-briefs", memoryH.HandleListContextBriefs)
		r.Post("/context-briefs", memoryH.HandleCreateContextBrief)
		r.Delete("/context-briefs/{id}", memoryH.HandleDeleteContextBrief)
		r.Get("/learned-patterns", memoryH.HandleListLearnedPatterns)
		r.Post("/learned-patterns/generate", memoryH.HandleGeneratePatterns)

		// Module services
		r.Get("/modules/services", analyticsH.HandleListModuleServices)
		r.Put("/modules/services/{id}", analyticsH.HandleUpdateModuleService)

		// Account management
		var rotatingEnc *crypto.RotatingEncryptor
		if cfg.Auth.EncryptionMasterKey != "" {
			var encErr error
			rotatingEnc, encErr = crypto.NewRotatingEncryptor(cfg.Auth.EncryptionMasterKey, "")
			if encErr != nil {
				slog.Error("create rotating encryptor for accounts", "error", encErr)
			}
		}
		accountH := NewAccountHandlers(pool, rotatingEnc)
		r.Route("/accounts", func(r chi.Router) {
			r.Get("/", accountH.HandleListAccounts)
			r.Post("/", accountH.HandleConnectIMAP)
			r.Delete("/{id}", accountH.HandleDeleteAccount)
		})

		// Device token management (push notifications)
		deviceHandlers := NewDeviceHandlers(pool)
		r.Route("/devices", func(r chi.Router) {
			r.Post("/register", deviceHandlers.RegisterDevice)
			r.Delete("/{token}", deviceHandlers.DeregisterDevice)
		})

		// Notification rules management
		rulesHandlers := NewRulesHandlers(pool)
		r.Route("/notification-rules", func(r chi.Router) {
			r.Get("/", rulesHandlers.ListRules)
			r.Post("/", rulesHandlers.CreateRule)
			r.Put("/{ruleID}", rulesHandlers.UpdateRule)
			r.Delete("/{ruleID}", rulesHandlers.DeleteRule)
		})

		// Billing handlers
		billingHandlers := NewBillingHandlers(pool, redisClient, billingCfg)
		usageSvc := billing.NewUsageService(pool, redisClient)
		usageHandlers := NewUsageHandlers(usageSvc)
		promoSvc := billing.NewPromoService(pool, redisClient)
		invoiceSvc := billing.NewInvoiceService(pool, redisClient)
		promoHandlers := NewPromoHandlers(promoSvc)
		invoiceHandlers := NewInvoiceHandlers(invoiceSvc)
		paymentHandlers := NewPaymentHandlers(pool)

		r.Route("/billing", func(r chi.Router) {
			r.Post("/checkout", billingHandlers.HandleCheckout)
			r.Post("/portal", billingHandlers.HandlePortal)
			r.Get("/subscription", billingHandlers.HandleGetSubscription)
			r.Get("/usage", usageHandlers.HandleGetUsage)

			r.Post("/promo/validate", promoHandlers.HandleValidatePromo)
			r.Post("/promo/apply", promoHandlers.HandleApplyPromo)

			r.Get("/invoices", invoiceHandlers.HandleListInvoices)
			r.Get("/invoices/{id}", invoiceHandlers.HandleGetInvoice)

			r.Get("/payment-methods", paymentHandlers.HandleListPaymentMethods)
			r.Post("/payment-methods/setup", paymentHandlers.HandleSetupPaymentMethod)
		})

		// Admin endpoints
		adminHandlers := NewAdminHandlers(pool)
		r.Route("/admin", func(r chi.Router) {
			r.Get("/connections", adminHandlers.ListConnections)

			// Admin promo management
			r.Get("/promo", promoHandlers.HandleListPromos)
			r.Post("/promo", promoHandlers.HandleCreatePromo)
			r.Patch("/promo/{id}", promoHandlers.HandleDeactivatePromo)
		})

		// Behavior Intelligence endpoints (Phase 8)
		behaviorSvc := behavior.NewBehaviorService(pool, redisClient, nil)
		behaviorH := NewBehaviorHandlers(behaviorSvc, pool)
		relationshipH := NewRelationshipHandlers(behaviorSvc)
		productivityH := NewProductivityHandlers(behaviorSvc)

		r.Route("/intelligence", func(r chi.Router) {
			// Overview requires basic_behavior (Attache+)
			r.With(mw.PlanGate("basic_behavior", redisClient, pool)).
				Get("/overview", behaviorH.HandleOverview)

			// Communication requires basic_behavior (Attache+)
			r.With(mw.PlanGate("basic_behavior", redisClient, pool)).
				Get("/communication", behaviorH.HandleCommunication)

			// WLB requires full_behavior (Privy Council+)
			r.With(mw.PlanGate("full_behavior", redisClient, pool)).
				Get("/wlb", behaviorH.HandleWLB)

			// Stress requires full_behavior (Privy Council+)
			r.With(mw.PlanGate("full_behavior", redisClient, pool)).
				Get("/stress", behaviorH.HandleStress)

			// Relationships requires full_behavior (Privy Council+)
			r.With(mw.PlanGate("full_behavior", redisClient, pool)).
				Get("/relationships", relationshipH.HandleListRelationships)

			// Productivity requires basic_behavior (Attache+)
			r.With(mw.PlanGate("basic_behavior", redisClient, pool)).
				Get("/productivity", productivityH.HandleProductivity)

			// Wellness reports require wellness (Privy Council+)
			r.With(mw.PlanGate("wellness", redisClient, pool)).
				Get("/wellness-reports", productivityH.HandleWellnessReports)

			// Manual compute trigger
			r.Post("/compute", behaviorH.HandleComputeNow)
		})

		// Behavior calibration settings
		r.With(mw.PlanGate("basic_behavior", redisClient, pool)).
			Put("/settings/behavior", behaviorH.HandleUpdateCalibration)

		// Calendar endpoints (Phase 9)
		calendarH := NewCalendarHandlers(pool, redisClient)
		r.Route("/calendar", func(r chi.Router) {
			// Calendar sync endpoints (Attache+)
			r.With(mw.PlanGate("calendar_sync", redisClient, pool)).
				Get("/events", calendarH.HandleEvents)
			r.With(mw.PlanGate("calendar_sync", redisClient, pool)).
				Get("/conflicts", calendarH.HandleConflicts)
			r.With(mw.PlanGate("calendar_sync", redisClient, pool)).
				Get("/connections", calendarH.HandleConnections)
			r.With(mw.PlanGate("calendar_sync", redisClient, pool)).
				Get("/preferences", calendarH.HandleGetPreferences)
			r.With(mw.PlanGate("calendar_sync", redisClient, pool)).
				Put("/preferences", calendarH.HandleUpdatePreferences)

			// Scheduling assist endpoints (Privy Council+)
			r.With(mw.PlanGate("scheduling_assist", redisClient, pool)).
				Get("/scheduling-requests", calendarH.HandleSchedulingRequests)
			r.With(mw.PlanGate("scheduling_assist", redisClient, pool)).
				Post("/suggest-slots", calendarH.HandleSuggestSlots)
			r.With(mw.PlanGate("scheduling_assist", redisClient, pool)).
				Post("/approve-slot", calendarH.HandleApproveSlot)

			// Meeting prep endpoints (Privy Council+)
			r.With(mw.PlanGate("meeting_prep", redisClient, pool)).
				Get("/meeting-briefs/{eventID}", calendarH.HandleGetBrief)
			r.With(mw.PlanGate("meeting_prep", redisClient, pool)).
				Post("/running-late/{eventID}", calendarH.HandleRunningLate)
			r.With(mw.PlanGate("meeting_prep", redisClient, pool)).
				Post("/meeting-notes/{eventID}", calendarH.HandleMeetingNotes)
		})

		// Task extraction endpoints (Phase 10)
		taskH := NewTaskHandlers(pool, redisClient)
		r.Route("/tasks", func(r chi.Router) {
			// Core task CRUD (Attache+)
			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Get("/", taskH.HandleListTasks)
			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Post("/", taskH.HandleCreateTask)
			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Get("/digest", taskH.HandleGetDigest)
			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Get("/stats", taskH.HandleGetStats)

			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Patch("/{id}", taskH.HandleUpdateTask)
			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Patch("/{id}/status", taskH.HandleUpdateStatus)
			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Delete("/{id}", taskH.HandleDismissTask)
			r.With(mw.PlanGate("task_extraction", redisClient, pool)).
				Post("/{id}/snooze", taskH.HandleSnooze)

			// Delegation endpoints (Privy Council+)
			r.With(mw.PlanGate("task_delegation", redisClient, pool)).
				Post("/{id}/delegate", taskH.HandleDelegate)
			r.With(mw.PlanGate("task_delegation", redisClient, pool)).
				Get("/{id}/delegations", taskH.HandleGetDelegations)
		})
	})

	return r, nil
}
