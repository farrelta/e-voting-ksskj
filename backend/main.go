package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type App struct {
	cfg            *Config
	db             *pgxpool.Pool
	rdb            *redis.Client
	sessions       *SessionStore
	otp            *OTPService
	ballotVerifier BallotVerifier
}

func (a *App) routes() http.Handler {
	api := http.NewServeMux()

	api.HandleFunc("POST /api/login", a.handleLogin)
	api.HandleFunc("POST /api/login/verify-otp", a.handleVerifyOTP)
	api.HandleFunc("POST /api/login/resend-otp", a.handleResendOTP)
	api.HandleFunc("GET /api/me", a.handleMe)
	api.HandleFunc("POST /api/logout", a.handleLogout)

	api.Handle("GET /api/elections", requireLogin(http.HandlerFunc(a.handleListElections)))
	api.Handle("GET /api/elections/{electionId}/candidates",
		requireVoter(http.HandlerFunc(a.handleListCandidates)))
	api.Handle("POST /api/elections/{electionId}/ballots",
		requireVoter(http.HandlerFunc(a.handleSubmitBallot)))
	api.HandleFunc("GET /api/verify/{receiptCode}", a.handleVerifyReceipt)

	api.Handle("POST /api/admin/elections",
		requireAdmin(http.HandlerFunc(a.handleCreateElection)))
	api.Handle("PUT /api/admin/elections/{electionId}",
		requireAdmin(http.HandlerFunc(a.handleUpdateElection)))
	api.Handle("DELETE /api/admin/elections/{electionId}",
		requireAdmin(http.HandlerFunc(a.handleDeleteElection)))
	api.Handle("POST /api/admin/elections/{electionId}/status",
		requireAdmin(http.HandlerFunc(a.handleUpdateElectionStatus)))
	api.Handle("POST /api/admin/candidates",
		requireAdmin(http.HandlerFunc(a.handleCreateCandidate)))
	api.Handle("GET /api/admin/elections/{electionId}/candidates",
		requireAdmin(http.HandlerFunc(a.handleListElectionCandidates)))
	api.Handle("PUT /api/admin/candidates/{candidateId}",
		requireAdmin(http.HandlerFunc(a.handleUpdateCandidate)))
	api.Handle("DELETE /api/admin/candidates/{candidateId}",
		requireAdmin(http.HandlerFunc(a.handleDeleteCandidate)))
	api.Handle("GET /api/admin/elections/{electionId}/voters",
		requireAdmin(http.HandlerFunc(a.handleListVoters)))
	api.Handle("POST /api/admin/elections/{electionId}/voters",
		requireAdmin(http.HandlerFunc(a.handleGrantEligibility)))
	api.Handle("DELETE /api/admin/elections/{electionId}/voters/{userId}",
		requireAdmin(http.HandlerFunc(a.handleRevokeEligibility)))

	if a.cfg.IsDev() {
		api.HandleFunc("GET /api/redis-test", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if err := a.rdb.Set(ctx, "evoting:test", "redis-is-working", time.Minute).Err(); err != nil {
				writeError(w, http.StatusInternalServerError, "Redis test failed")
				return
			}
			v, err := a.rdb.Get(ctx, "evoting:test").Result()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Redis test failed")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"redis": v})
		})
	}

	root := http.NewServeMux()
	root.Handle("/api/", a.loadSession(api))
	root.Handle("/", http.FileServer(http.Dir(a.cfg.FrontendDir)))
	return root
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()

	pool, err := newDB(ctx, cfg)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	rdb, err := newRedis(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	defer rdb.Close()
	log.Println("Redis connected")

	app := &App{
		cfg:      cfg,
		db:       pool,
		rdb:      rdb,
		sessions: newSessionStore(rdb, cfg.IsProduction()),
		otp:      newOTPService(rdb, cfg.OTPSecret),
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("Server running at http://localhost:%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}
