// Command vulos-talk is the standalone Vulos Talk product: team chat with
// channels/"Spaces", DMs, threads, and real-time huddles/meetings.
//
// It is extracted from vulos-office and mirrors office's conventions: a Go
// (gin) backend that serves the meeting + spaces API and embeds the built React
// SPA via //go:embed dist. It runs COMPLETELY STANDALONE — identity is verified
// against a local JWT secret, entitlements are unlimited (self-host), and usage
// metering is a no-op (the integration seam). The vulos-cloud control plane is
// optional and engaged only when VULOS_CP_BASE_URL is set, in which case the
// backend/integration/cloud adapter resolves entitlements and reports usage
// against the cp (mirroring vulos-office). The core never imports that adapter —
// only this composition root does — so removing it can never break the
// standalone build.
//
// TODO(seam-C): route huddle video through vulos-meet — today Talk hosts its
// own WebRTC meeting/lobby/TURN backend (carried from office). The product map
// consolidates real-time video into the dedicated vulos-meet product; when that
// lands, replace the /meetings + /meet + /turn surface with a seam-C handoff to
// vulos-meet and keep only chat/spaces here.
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
	"vulos-talk/backend/middleware"
	"vulos-talk/backend/obs"
	"vulos-talk/backend/seam"
	"vulos-talk/backend/services/meeting"
	"vulos-talk/backend/storage"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/vul-os/vulos-apps/appsplatform"
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

	store, err := storage.New(cfg)
	if err != nil {
		log.Fatal("Storage init failed:", err)
	}

	// Durable lobby/meeting store (file-backed SQLite, survives restarts).
	lobbyDSN := os.Getenv("VULOS_LOBBY_DB")
	if lobbyDSN == "" {
		lobbyDSN = cfg.Server.DataDir + "/lobby.db"
	}
	if err := meeting.InitDefault(lobbyDSN); err != nil {
		log.Fatalf("Lobby store init failed (%s): %v", lobbyDSN, err)
	}
	log.Printf("Lobby store → %s", lobbyDSN)

	// Org-bucket object store (for meeting recordings). Boots without cloud.
	storage.InitOrgBucket()

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

	// Short-lived TURN/ICE credentials for WebRTC huddles.
	turnHandler := handlers.NewTURNHandler()
	protected.GET("/turn/credentials", turnHandler.Credentials)

	// Meetings: unified rooms with lobby, signed tokens, organizer-only controls.
	meetingHandler := handlers.NewMeetingHandler(store)
	protected.POST("/meetings", meetingHandler.Create)
	protected.GET("/meetings", meetingHandler.List)
	protected.GET("/meetings/:id", meetingHandler.Get)
	protected.PUT("/meetings/:id", meetingHandler.Update)
	protected.DELETE("/meetings/:id", meetingHandler.Delete)
	// Join is public — external invitees follow a bare link with no Vulos account.
	api.GET("/meetings/:id/join", meetingHandler.Join)

	meetJoinHandler := handlers.NewMeetJoinHandler(store)
	api.POST("/meet/:roomId/token", meetJoinHandler.IssueToken)
	api.POST("/meet/:roomId/lobby/enter", meetJoinHandler.LobbyEnter)
	protected.GET("/meet/:roomId/lobby", meetJoinHandler.LobbyList)
	protected.POST("/meet/:roomId/admit", meetJoinHandler.Admit)
	protected.POST("/meet/:roomId/admit-all", meetJoinHandler.AdmitAll)
	protected.POST("/meet/:roomId/deny", meetJoinHandler.Deny)

	// Recordings: authenticated, membership-checked storage writes/reads.
	recordingHandler := handlers.NewRecordingHandler(store)
	protected.POST("/meet/:roomId/recordings", recordingHandler.Upload)
	protected.GET("/meet/:roomId/recordings", recordingHandler.List)
	protected.GET("/meet/:roomId/recordings/:rid", recordingHandler.Download)
	protected.DELETE("/meet/:roomId/recordings/:rid", recordingHandler.Delete)

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
