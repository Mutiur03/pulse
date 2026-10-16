package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/api"
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/artifacts"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/dashboards"
	"github.com/Mutiur03/pulse/internal/governance"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/orgs"
	"github.com/Mutiur03/pulse/internal/projects"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/safehttp"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
	"github.com/Mutiur03/pulse/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	cfg.ServiceName = "query-api"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Printf("Failed to initialize tracer: %v", err)
	} else {
		defer func() { _ = cleanup(context.Background()) }()
	}

	chClient, err := ch.NewClient(cfg.ClickHouseAddr)
	if err != nil {
		log.Fatalf("Failed to create ClickHouse client: %v", err)
	}
	defer func() { _ = chClient.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	issueStore, err := issues.Connect(ctx, cfg.PostgresURL)
	cancel()
	if err != nil {
		log.Fatalf("Failed to connect to Postgres: %v", err)
	}
	defer issueStore.Close()

	db, err := pgxpool.New(context.Background(), cfg.PostgresURL)
	if err != nil {
		log.Fatalf("Failed to configure Postgres: %v", err)
	}
	defer db.Close()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() { _ = rdb.Close() }()
	idle := auth.DefaultIdle
	if h := envInt("SESSION_IDLE_HOURS", 0); h > 0 {
		idle = time.Duration(h) * time.Hour
	}
	accounts := &auth.Store{DB: db, Idle: idle}
	proxies, err := clientip.Parse(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		log.Fatalf("TRUSTED_PROXIES: %v", err)
	}
	outbound, err := safehttp.ParseAllowlist(os.Getenv("OUTBOUND_ALLOWLIST"))
	if err != nil {
		log.Fatalf("OUTBOUND_ALLOWLIST: %v", err)
	}
	keyStore := &keys.Store{DB: db, Redis: rdb}
	orgStore := &orgs.Store{DB: db}
	auditStore := &audit.Store{DB: db}
	limit := func(prefix, env string, def int, window time.Duration) *auth.Throttle {
		return &auth.Throttle{Redis: rdb, Max: envInt(env, def), Window: window, Prefix: prefix}
	}
	srv := &api.Server{
		Traces: chClient, Issues: issueStore, Accounts: accounts,
		Keys:     keyStore,
		Projects: &projects.Store{DB: db},
		Limits: api.AuthLimits{
			// Five failed logins per email and address lock it for 15 minutes,
			// twenty from any addresses lock the email.
			LoginFail:  limit("login:fail", "LOGIN_FAIL_LIMIT", 5, 15*time.Minute),
			LoginEmail: limit("login:email", "LOGIN_EMAIL_FAIL_LIMIT", 20, 15*time.Minute),
			// Attempts per client address, successful or not.
			LoginIP:  limit("login:ip", "LOGIN_IP_LIMIT", 100, 15*time.Minute),
			SignupIP: limit("signup:ip", "SIGNUP_IP_LIMIT", 20, time.Hour),
			ResetIP:  limit("reset:ip", "RESET_IP_LIMIT", 20, time.Hour),
			// Five reset emails per address and client per hour.
			ResetEmail: limit("reset:req", "RESET_EMAIL_LIMIT", 5, time.Hour),
		},
		ClientIP:      proxies,
		Sessions:      accounts,
		Roles:         orgStore,
		Origins:       &projects.Origins{DB: db, Keys: keyStore},
		Audit:         auditStore,
		AuditLog:      auditStore,
		OutboundCheck: outbound.CheckURL,
		Orgs:          orgStore,
		// Without SMTP_ADDR emails are written to the log.
		Mail: mail.New(mail.Config{
			Addr: os.Getenv("SMTP_ADDR"), From: os.Getenv("SMTP_FROM"),
			User: os.Getenv("SMTP_USER"), Password: os.Getenv("SMTP_PASSWORD"),
		}),
		AppURL:     os.Getenv("APP_URL"),
		Replays:    chClient,
		Metrics:    chClient,
		Insights:   chClient,
		Analytics:  chClient,
		Logs:       chClient,
		Governance: &governance.Store{DB: db},
		Persons:    chClient,
		Dashboards: &dashboards.Store{DB: db},
		// Set COOKIE_SECURE=false only for plain-HTTP local development.
		SecureCookie: os.Getenv("COOKIE_SECURE") != "false",
	}

	srv.Alerts, srv.AlertMetrics = &alerts.Store{DB: db}, chClient
	srv.AlertSender = &alerts.Notifier{Mail: srv.Mail, AppURL: srv.AppURL, Policy: outbound}

	// A typed nil would look non-nil through the interface.
	if objects := replay.Connect(); objects != nil {
		srv.ReplayObjects = objects
	}
	// Releases and upload tokens work without object storage; uploads and
	// symbolication need it.
	rel := &artifacts.Store{DB: db}
	srv.Releases = rel
	if objects := replay.ConnectArtifacts(); objects != nil {
		srv.ArtifactObjects = objects
		srv.Symbolicator = &symbolicate.Symbolicator{Artifacts: rel, Objects: objects}
	}
	if d, err := strconv.Atoi(os.Getenv("REPLAY_RETENTION_DAYS")); err == nil {
		srv.ReplayRetentionDays = d
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// The standard logger prints full URLs; this one redacts tokens.
	r.Use(api.RequestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(api.SecurityHeaders(envInt("HSTS_MAX_AGE", 0)))
	// The web app calls this API through its own same-origin proxy, so no
	// CORS headers are sent and the session cookie never leaves that origin.
	// State-changing requests must come from the web app's origin.
	csrf := auth.CSRF{}
	for _, o := range append([]string{os.Getenv("APP_URL")}, strings.Split(os.Getenv("CSRF_TRUSTED_ORIGINS"), ",")...) {
		if n := auth.NormalizeOrigin(o); n != "" {
			csrf.Origins = append(csrf.Origins, n)
		}
	}
	if len(csrf.Origins) == 0 {
		log.Printf("APP_URL is not set: the CSRF Origin check only rejects requests browsers mark as cross-site")
	}
	r.Use(auth.Middleware(accounts, csrf))

	handler := gen.HandlerFromMux(srv, r)

	addr := fmt.Sprintf(":%d", cfg.QueryAPIPort)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("Query API listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}

// envInt reads a positive integer setting, or def.
func envInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil && n >= 0 {
		return n
	}
	return def
}
