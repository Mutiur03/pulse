package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	"github.com/Mutiur03/pulse/internal/logging"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/monitor"
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
	logging.Setup("query-api", cfg.LogLevel)
	cfg.ServiceName = "query-api"
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		slog.Error("query API stopped", "err", err)
		os.Exit(1)
	}
}

// deps are the connections the API is built from.
type deps struct {
	ch       *ch.Client
	issues   *issues.Store
	db       *pgxpool.Pool
	redis    *redis.Client
	proxies  *clientip.Resolver
	outbound *safehttp.Policy
}

// run connects to every dependency and serves the API until ctx ends.
func run(ctx context.Context, cfg *config.Config) error {
	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		slog.Error("failed to initialize tracer", "err", err)
	} else {
		defer func() { _ = cleanup(context.Background()) }()
	}

	var d deps
	if d.proxies, err = clientip.Parse(config.String("TRUSTED_PROXIES", "")); err != nil {
		return fmt.Errorf("TRUSTED_PROXIES: %w", err)
	}
	if d.outbound, err = safehttp.ParseAllowlist(cfg.OutboundAllowlist); err != nil {
		return fmt.Errorf("OUTBOUND_ALLOWLIST: %w", err)
	}
	redisOpts, err := cfg.RedisOptions()
	if err != nil {
		return fmt.Errorf("invalid REDIS_URL: %w", err)
	}

	// Cost limits for every read; see docs/load-testing.md.
	ch.HeavyMaxThreads = config.Int("QUERY_HEAVY_MAX_THREADS", ch.HeavyMaxThreads)
	if d.ch, err = ch.Connect(chOptions(cfg)); err != nil {
		return fmt.Errorf("create ClickHouse client: %w", err)
	}
	defer func() { _ = d.ch.Close() }()

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	d.issues, err = issues.Connect(cctx, cfg.PostgresURL)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to Postgres: %w", err)
	}
	defer d.issues.Close()

	if d.db, err = pgxpool.New(ctx, cfg.PostgresURL); err != nil {
		return fmt.Errorf("configure Postgres: %w", err)
	}
	defer d.db.Close()
	d.redis = redis.NewClient(redisOpts)
	defer func() { _ = d.redis.Close() }()

	srv, accounts := newServer(cfg, d)
	handler := gen.HandlerFromMux(srv, newRouter(cfg, accounts))

	// Metrics are served on their own port, not the public one.
	monitor.Service("query-api")
	monitor.Serve(config.String("METRICS_ADDR", ":9102"))
	return serve(ctx, fmt.Sprintf(":%d", cfg.QueryAPIPort), handler)
}

// newServer builds the API handlers and stores on top of d.
func newServer(cfg *config.Config, d deps) (*api.Server, *auth.Store) {
	db, rdb, chClient, issueStore, proxies, outbound := d.db, d.redis, d.ch, d.issues, d.proxies, d.outbound
	idle := auth.DefaultIdle
	if h := config.NonNegInt("SESSION_IDLE_HOURS", 0); h > 0 {
		idle = time.Duration(h) * time.Hour
	}
	accounts := &auth.Store{DB: db, Idle: idle}
	keyStore := &keys.Store{DB: db, Redis: rdb}
	orgStore := &orgs.Store{DB: db}
	auditStore := &audit.Store{DB: db}
	limit := func(prefix, env string, def int, window time.Duration) *auth.Throttle {
		return &auth.Throttle{Redis: rdb, Max: config.NonNegInt(env, def), Window: window, Prefix: prefix}
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
		Mail:       mail.New(cfg.SMTP),
		AppURL:     cfg.AppURL,
		Replays:    chClient,
		Metrics:    chClient,
		Insights:   chClient,
		Analytics:  chClient,
		Logs:       chClient,
		Governance: &governance.Store{DB: db},
		Persons:    chClient,
		Dashboards: &dashboards.Store{DB: db},
		// Set COOKIE_SECURE=false only for plain-HTTP local development.
		SecureCookie: config.String("COOKIE_SECURE", "") != "false",
	}

	srv.Alerts, srv.AlertMetrics = &alerts.Store{DB: db}, chClient
	srv.AlertSender = &alerts.Notifier{Mail: srv.Mail, AppURL: srv.AppURL, Policy: outbound}

	// A typed nil would look non-nil through the interface.
	if objects := replay.Connect(cfg.S3); objects != nil {
		srv.ReplayObjects = objects
	}
	// Releases and upload tokens work without object storage; uploads and
	// symbolication need it.
	rel := &artifacts.Store{DB: db}
	srv.Releases = rel
	if objects := replay.ConnectArtifacts(cfg.S3, cfg.ArtifactBucket); objects != nil {
		srv.ArtifactObjects = objects
		srv.Symbolicator = &symbolicate.Symbolicator{Artifacts: rel, Objects: objects}
	}
	srv.ReplayRetentionDays = config.Int("REPLAY_RETENTION_DAYS", 0)

	return srv, accounts
}

// newRouter returns the middleware stack every API request passes through.
func newRouter(cfg *config.Config, accounts *auth.Store) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(monitor.Middleware("query-api", func(r *http.Request) string {
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			return rc.RoutePattern()
		}
		return "other"
	}))
	// The standard logger prints full URLs; this one redacts tokens.
	r.Use(api.RequestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(api.SecurityHeaders(config.NonNegInt("HSTS_MAX_AGE", 0)))
	// The web app calls this API through its own same-origin proxy, so no
	// CORS headers are sent and the session cookie never leaves that origin.
	// State-changing requests must come from the web app's origin.
	csrf := auth.CSRF{}
	for _, o := range append([]string{cfg.AppURL}, config.List("CSRF_TRUSTED_ORIGINS")...) {
		if n := auth.NormalizeOrigin(o); n != "" {
			csrf.Origins = append(csrf.Origins, n)
		}
	}
	if len(csrf.Origins) == 0 {
		slog.Warn("APP_URL is not set: the CSRF Origin check only rejects requests browsers mark as cross-site")
	}
	r.Use(auth.Middleware(accounts, csrf))
	limiter := &api.QueryLimiter{
		PerOrg:  config.NonNegInt("QUERY_MAX_CONCURRENT_PER_ORG", 8),
		Wait:    time.Duration(config.NonNegInt("QUERY_QUEUE_WAIT_SECONDS", 10)) * time.Second,
		Timeout: time.Duration(config.NonNegInt("QUERY_MAX_EXECUTION_SECONDS", 30)) * time.Second,
	}
	r.Use(limiter.Middleware)

	return r
}

// serve runs the HTTP server until ctx ends or it fails.
func serve(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() {
		slog.Info("query API listening", "addr", addr)
		errc <- server.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(sctx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}
	return nil
}

// chOptions adds the per-query cost limits; see docs/load-testing.md.
func chOptions(cfg *config.Config) ch.Options {
	o := cfg.ClickHouse
	o.Limits = ch.ReadLimits{
		MaxExecutionTime: time.Duration(config.NonNegInt("QUERY_MAX_EXECUTION_SECONDS", 30)) * time.Second,
		MaxRowsToRead:    uint64(config.NonNegInt("QUERY_MAX_ROWS_TO_READ", 2_000_000_000)),
		MaxMemoryUsage:   uint64(config.NonNegInt("QUERY_MAX_MEMORY_MB", 4096)) << 20,
	}
	return o
}
