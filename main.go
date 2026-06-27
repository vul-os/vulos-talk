// Command vulos-talk is the standalone Vulos Talk product: team chat with
// channels/"Spaces", DMs, threads, and huddles.
//
// It is a Go (gin) backend that serves the Spaces API and embeds the built React
// SPA via //go:embed dist. It runs COMPLETELY STANDALONE — identity is verified
// against a local JWT secret, entitlements are unlimited (self-host), and usage
// metering is a no-op (the integration seam). The vulos-cloud control plane is
// optional and engaged only when VULOS_CP_BASE_URL is set, in which case the
// backend/integration/cloud adapter resolves entitlements and reports usage
// against the cp. The core never imports that adapter — only this composition
// root does — so removing it can never break the standalone build.
//
// Seam-C (real-time video): Talk does NOT host audio/video. Starting a huddle in
// a channel hands the member off to the dedicated vulos-meet product — Talk mints
// a VULOS-MEET/1 join token (locally, or brokered via the control plane) and the
// SPA embeds the Meet web client in an iframe, with Meet's in-call chat pointed
// back at the originating Talk channel. Meet is an OPTIONAL dependency: with no
// Meet configured the huddle action degrades to a "video not configured" state
// and Talk standalone (chat + Spaces) is fully functional. See backend/meet.
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"vulos-talk/backend/billing"
	"vulos-talk/backend/config"
	"vulos-talk/backend/handlers"
	"vulos-talk/backend/integration/cloud"
	"vulos-talk/backend/meet"
	"vulos-talk/backend/middleware"
	"vulos-talk/backend/obs"
	"vulos-talk/backend/seam"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/vul-os/vulos-apps/appsplatform"
	"github.com/vul-os/vulos-apps/mcp"
)

// Version is set at build time via -ldflags "-X main.Version=vX.Y.Z".
var Version = "dev"

//go:embed all:dist
var distFS embed.FS

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		log.Println(Version)
		return
	}

	log.Printf("vulos-talk %s starting", Version)
	obs.Init()

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Printf("Config error: %v — using defaults", err)
		cfg = config.Default()
	}

	// Fail closed: when auth is enabled, refuse to start without a JWT signing
	// secret so we never ship with a predictable key.
	if cfg.Auth.Enabled && !middleware.JWTSecretConfigured() {
		log.Fatalf("auth is enabled but no JWT signing secret is configured: set %s "+
			"to a strong random value (or %s=1 for local dev)",
			middleware.EnvJWTSecret, middleware.EnvDevMode)
	}

	// Integration seam: standalone by default; cloud control plane is optional
	// and wired only when configured (VULOS_CP_BASE_URL). The core never imports
	// the cloud adapter — only this composition root does — so deleting the
	// adapter can never break the standalone build.
	provider := seam.NewStandaloneProvider(middleware.JWTSecret, cfg.Auth.Enabled)
	if cloud.Enabled() {
		ccfg := cloud.FromEnv()
		// Identity stays locally-verified (HS256) and is org-stamped; entitlements
		// and usage are resolved/reported against the control plane.
		provider = cloud.NewProvider(ccfg, provider.Identity)
		log.Printf("[seam] integration mode: cloud (control plane %s)", ccfg.BaseURL)
	} else {
		log.Printf("[seam] integration mode: standalone (no control plane)")
	}
	billing.Configure(provider)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// CORS: explicit origin allowlist (credentialed) when VULOS_TALK_CORS_ORIGINS
	// is set; otherwise allow all origins WITHOUT credentials (same-origin SPA).
	corsCfg := cors.Config{
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Authorization", "X-Registration-Token", "X-Account-ID"},
	}
	if raw := strings.TrimSpace(os.Getenv("VULOS_TALK_CORS_ORIGINS")); raw != "" {
		var origins []string
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
		corsCfg.AllowOrigins = origins
		corsCfg.AllowCredentials = true
		log.Printf("[cors] explicit origin allowlist: %v (credentials allowed)", origins)
	} else {
		corsCfg.AllowAllOrigins = true
		log.Printf("[cors] no VULOS_TALK_CORS_ORIGINS set; allowing all origins WITHOUT credentials")
	}
	r.Use(cors.New(corsCfg))

	// Prometheus metrics + version (no auth).
	r.GET("/metrics", gin.WrapH(obs.Handler()))
	r.GET("/version", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"version": Version})
	})

	// Auth status surface (unauthenticated). Talk does not host login — the
	// shell redirects to the central identity surface on 401.
	authHandler := handlers.NewAuthHandler(cfg)
	api := r.Group("/api")
	api.GET("/auth/status", authHandler.Status)
	api.GET("/auth/me", authHandler.Me)

	// Protected API routes.
	protected := api.Group("/")
	if cfg.Auth.Enabled {
		protected.Use(middleware.Auth(cfg))
	}

	// Presence (REST/poll heartbeat + roster) for Spaces.
	presenceHandler := handlers.NewPresenceHandler()
	protected.POST("/spaces/presence/heartbeat", presenceHandler.Heartbeat)
	protected.GET("/spaces/presence/roster", presenceHandler.Roster)

	// Spaces: channels, DMs, threads, messages, reactions, pins, status, search.
	spacesHandler := handlers.NewSpacesHandlerExt()
	protected.GET("/spaces/channels", spacesHandler.ListChannels)
	protected.POST("/spaces/channels", spacesHandler.CreateChannel)
	protected.POST("/spaces/channels/:channelId/join", spacesHandler.JoinChannel)
	protected.GET("/spaces/channels/:channelId/members", spacesHandler.ListMembers)
	protected.POST("/spaces/channels/:channelId/members", spacesHandler.InviteMember)
	protected.PUT("/spaces/channels/:channelId/members/me/name", spacesHandler.SetMyDisplayName)
	protected.GET("/spaces/channels/:channelId/messages", spacesHandler.ListMessages)
	protected.POST("/spaces/channels/:channelId/messages", spacesHandler.SendMessage)
	protected.PUT("/spaces/channels/:channelId/messages/:msgId", spacesHandler.EditMessage)
	protected.DELETE("/spaces/channels/:channelId/messages/:msgId", spacesHandler.DeleteMessage)
	protected.POST("/spaces/channels/:channelId/read", spacesHandler.MarkRead)
	protected.GET("/spaces/channels/:channelId/read", spacesHandler.GetReadState)
	protected.GET("/spaces/channels/:channelId/ops", spacesHandler.ExportOps)
	protected.POST("/spaces/ops", spacesHandler.MergeOps)
	protected.GET("/spaces/channels/:channelId/reactions", spacesHandler.ListReactions)
	protected.POST("/spaces/messages/:msgId/react", spacesHandler.React)
	protected.DELETE("/spaces/messages/:msgId/react", spacesHandler.Unreact)
	protected.GET("/spaces/channels/:channelId/pins", spacesHandler.ListPins)
	protected.POST("/spaces/channels/:channelId/pins", spacesHandler.PinMessage)
	protected.DELETE("/spaces/channels/:channelId/pins/:msgId", spacesHandler.UnpinMessage)
	protected.PUT("/spaces/users/me/status", spacesHandler.SetStatus)
	protected.GET("/spaces/users/:userId/status", spacesHandler.GetStatus)
	protected.GET("/spaces/channels/:channelId/search", spacesHandler.SearchMessages)
	protected.GET("/spaces/channels/:channelId/threads/:parentId", spacesHandler.ListThread)
	protected.POST("/spaces/channels/:channelId/threads/:parentId/reply", spacesHandler.ReplyThread)

	// Seam-C: huddles hand off to vulos-meet (no A/V hosted here). GET reports
	// whether video is configured (the SPA disables the action otherwise); POST
	// mints a per-channel Meet join (membership-checked) and returns a deep link
	// the SPA embeds in an iframe with Talk-backed in-call chat. See backend/meet.
	meetCfg := meet.FromEnv()
	if meetCfg.Enabled() {
		log.Printf("[seam-C] huddles → vulos-meet (mode=%s)", meetCfg.Mode())
	} else {
		log.Printf("[seam-C] huddles disabled (no vulos-meet configured); chat + Spaces only")
	}
	huddleHandler := handlers.NewHuddleHandler(spacesHandler.Store(), cfg, meetCfg)
	protected.GET("/meet/config", huddleHandler.Config)
	protected.POST("/spaces/channels/:channelId/huddle", huddleHandler.Start)

	// -----------------------------------------------------------------------
	// Apps & Bots platform: the shared @vulos/apps platform Talk now hosts as
	// its "apps & bots place" — registry (the seam), dispatcher, the Talk
	// ProductAdapter, the management API (GET /api/apps, the consolidation
	// contract Vulos Workspace reads), the runtime app-token API, slash
	// commands, incoming webhooks, and the SSE event stream. It generalizes
	// Talk's original bespoke bot framework; the legacy /api/bot/v1 + /api/bots
	// surface is kept as a compat shim over the SAME registry.
	//
	// Open-core seam: the standalone registry (pure-Go SQLite, env DSN) is the
	// default. A Vulos Cloud apps control plane would implement the SAME
	// appsplatform.Registry in a separate package wired ONLY here and ONLY when
	// explicitly selected (env-gated) — the core never imports it, mirroring the
	// integration seam above.
	// -----------------------------------------------------------------------
	appsDSN := os.Getenv("VULOS_APPS_DB")
	if appsDSN == "" {
		appsDSN = os.Getenv("VULOS_BOTS_DB") // back-compat with the pre-migration env var
	}
	if appsDSN == "" {
		appsDSN = cfg.Server.DataDir + "/apps.db"
	}
	if useCloudAppsRegistry() {
		// Env-gated cloud control-plane hook. No cloud apps registry is compiled
		// into this build, so we log and fall back to standalone rather than
		// importing a cloud package into the core. A deployment that wants the
		// cloud developer console wires `reg = cloudapps.New(...)` right here.
		log.Printf("[apps] cloud apps registry requested (VULOS_APPS_CLOUD) but not compiled in; using standalone")
	}
	var appsRegistry appsplatform.Registry
	if reg, err := appsplatform.NewStandaloneRegistry(appsDSN); err != nil {
		log.Printf("apps: durable registry unavailable (%v); using in-memory registry", err)
		appsRegistry = appsplatform.NewMemoryRegistry()
	} else {
		appsRegistry = reg
		log.Printf("Apps registry → %s", appsDSN)
	}

	appsDispatcher := appsplatform.NewDispatcher(appsRegistry, appsplatform.ProductTalk)
	talkAdapter := handlers.NewTalkAdapter(spacesHandler)
	appsSink := handlers.NewAppsSink(appsRegistry, appsDispatcher, talkAdapter)
	// Hook the dispatcher into the send/reply/join path and slash-command dispatch.
	spacesHandler.SetBotSink(appsSink)

	// New canonical surface: the platform's mountable handler set. Management is
	// authed by Talk's OWN session (AppsAdminAuth); runtime by Bearer app token.
	appsHandler, err := appsplatform.NewHandler(appsplatform.MountConfig{
		Adapter:    talkAdapter,
		Registry:   appsRegistry,
		Dispatcher: appsDispatcher,
		Admin:      middleware.AppsAdminAuth(cfg),
		BasePath:   "/api/apps",
	})
	if err != nil {
		log.Fatalf("apps platform mount failed: %v", err)
	}
	// Mount the raw net/http handler at the base + its subtree (the platform owns
	// its own auth, so it is not behind the gin protected group).
	r.Any("/api/apps", gin.WrapH(appsHandler))
	r.Any("/api/apps/*any", gin.WrapH(appsHandler))

	// MCP surface: the SAME Talk adapter + registry, a different shape over the
	// seam. Lets any LLM/agent operate Talk over the Model Context Protocol
	// (JSON-RPC over Streamable HTTP) — Act actions become MCP tools, Read kinds
	// become MCP resources, authed by the SAME Bearer app token (vat_). The
	// cloud-aggregation gateway is left as an env-gated seam (MCPConfig.Gateway);
	// the open-core build wires none, so it runs standalone.
	mcpHandler, err := mcp.NewHandler(mcp.MCPConfig{
		Adapter:  talkAdapter,
		Registry: appsRegistry,
		Emit:     appsDispatcher.EmitFunc(),
		BasePath: "/mcp",
	})
	if err != nil {
		log.Fatalf("mcp mount failed: %v", err)
	}
	r.Any("/mcp", gin.WrapH(mcpHandler))
	r.Any("/mcp/*any", gin.WrapH(mcpHandler))

	// Legacy admin API (session-authed, owner-scoped) — COMPAT shim over the
	// same registry; the canonical surface is /api/apps.
	botsHandler := handlers.NewBotsHandler(appsRegistry)
	protected.GET("/bots", botsHandler.List)
	protected.POST("/bots", botsHandler.Create)
	protected.GET("/bots/:id", botsHandler.Get)
	protected.PUT("/bots/:id", botsHandler.Update)
	protected.DELETE("/bots/:id", botsHandler.Delete)
	protected.POST("/bots/:id/rotate-token", botsHandler.RotateToken)
	protected.POST("/bots/:id/rotate-secret", botsHandler.RotateSecret)
	// Slash-command catalog for the composer autocomplete (session-authed).
	protected.GET("/spaces/commands", botsHandler.Commands)

	// Legacy bot REST API (Bearer app-token authed) — COMPAT shim so the
	// published BOT-API + the echo-bot example keep working.
	botAPIHandler := handlers.NewBotAPIHandler(spacesHandler, appsRegistry, appsSink)
	botV1 := api.Group("/bot/v1")
	botV1.Use(middleware.BotAuth(appsRegistry))
	botV1.GET("/auth.test", botAPIHandler.AuthTest)
	botV1.POST("/messages", botAPIHandler.PostMessage)
	botV1.GET("/channels", botAPIHandler.ListChannels)
	botV1.GET("/channels/:channelId/history", botAPIHandler.History)
	botV1.GET("/channels/:channelId/members", botAPIHandler.Members)
	botV1.POST("/reactions", botAPIHandler.AddReaction)
	botV1.DELETE("/reactions", botAPIHandler.RemoveReaction)
	// Socket-mode style SSE event stream.
	botEventsHandler := handlers.NewBotEventsHandler(appsDispatcher)
	botV1.GET("/events", botEventsHandler.Stream)

	// Incoming webhooks: NO auth header — the webhook id in the path is the secret.
	api.POST("/bot/hooks/:webhookId", botAPIHandler.IncomingWebhook)

	// Serve the embedded SPA (history-API fallback to index.html).
	staticFS, err := fs.Sub(distFS, "dist")
	if err != nil {
		log.Fatal("Failed to create static FS:", err)
	}
	staticServer := http.FileServer(http.FS(staticFS))
	r.NoRoute(func(c *gin.Context) {
		fsPath := strings.TrimPrefix(c.Request.URL.Path, "/")
		if f, err := staticFS.Open(fsPath); err == nil {
			f.Close()
			staticServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Request.URL.Path = "/"
		staticServer.ServeHTTP(c.Writer, c.Request)
	})

	addr := cfg.Server.Addr
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("Vulos Talk running → http://localhost%s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

// useCloudAppsRegistry reports whether the operator asked to broker apps through
// a Vulos Cloud control plane (env-gated). The open-core seam: the core never
// imports a cloud apps package; only this composition root would wire one when
// this returns true.
func useCloudAppsRegistry() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("VULOS_APPS_CLOUD"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
