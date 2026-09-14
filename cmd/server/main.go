package main

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dipak0000812/orchestrix/internal/api"
	"github.com/dipak0000812/orchestrix/internal/auth"
	"github.com/dipak0000812/orchestrix/internal/config"
	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/dependency"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/metrics"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
	"github.com/dipak0000812/orchestrix/internal/worker"
)

const (
	maxRequestBodyBytes = 1 << 20 // 1 MiB
	knownDevPassword    = "orchestrix_dev_password"
)

func main() {
	log.Println("Starting Orchestrix...")

	cfg, err := config.Load(getEnv("CONFIG_PATH", "configs/base.yaml"))
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	dbHost := getEnv("DB_HOST", "localhost")
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		log.Fatal("DB_PASSWORD environment variable is required")
	}
	dbSSLMode := getEnv("DB_SSLMODE", "disable")
	warnIfInsecureDBConfig(dbHost, dbSSLMode, dbPassword)

	dbConfig := repository.DBConfig{
		Host:            dbHost,
		Port:            getEnvInt("DB_PORT", 5434),
		User:            getEnv("DB_USER", "orchestrix"),
		Password:        dbPassword,
		Database:        getEnv("DB_NAME", "orchestrix_dev"),
		SSLMode:         dbSSLMode,
		MaxConnections:  20,
		MinConnections:  2,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
	}

	pool, err := repository.NewConnectionPool(context.Background(), dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer repository.ClosePool(pool)
	log.Println("Connected to database")

	repo := repository.NewPostgresJobRepository(pool)
	stateMachine := state.NewStateMachine()
	idGen := service.NewULIDGenerator()
	retryConfig := service.DefaultRetryConfig()
	resolver := dependency.NewResolver(repo)
	jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig, resolver)

	keyRepo := auth.NewKeyRepository(pool)
	if err := bootstrapAPIKey(context.Background(), keyRepo); err != nil {
		log.Fatalf("Failed to bootstrap API key: %v", err)
	}

	executors := executor.NewExecutorRegistry()
	executors.Register("demo_job", executor.NewDemoExecutor(1*time.Second))
	executors.Register("http_webhook", executor.NewSSRFSafeWebhookExecutor(10*time.Second))
	executors.Register("compute_checksum", executor.NewChecksumExecutor())
	log.Println("Registered executors: demo_job, http_webhook, compute_checksum")

	jobChannel := make(chan *model.Job, 200)
	m := metrics.NewMetrics()

	sched := scheduler.NewScheduler(
		repo,
		1*time.Second,
		50,
		jobChannel,
	)
	sched.EnableAdaptivePolling()
	sched.Start()
	defer sched.Stop()

	workers := worker.NewWorkerPool(
		5,
		jobChannel,
		executors,
		jobService,
		m,
		10*time.Second,
	)
	workers.Start()
	defer workers.Stop()

	handler := api.NewHandler(jobService, m)

	router := http.NewServeMux()
	router.HandleFunc("POST /api/v1/jobs", handler.CreateJob)
	router.HandleFunc("GET /api/v1/jobs/{id}", handler.GetJob)
	router.HandleFunc("GET /api/v1/jobs", handler.ListJobs)
	router.HandleFunc("DELETE /api/v1/jobs/{id}", handler.CancelJob)
	router.HandleFunc("GET /health", handler.Health)
	router.Handle("GET /metrics", promhttp.Handler())

	bodyLimited := api.MaxBodySizeMiddleware(maxRequestBodyBytes)(router)
	authedRouter := auth.Middleware(keyRepo, map[string]bool{"/health": true})(bodyLimited)
	server := &http.Server{
		Addr:    ":" + strconv.Itoa(cfg.Server.Port),
		Handler: authedRouter,
	}

	go func() {
		log.Printf("HTTP server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Shutdown.Timeout.Std())
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func warnIfInsecureDBConfig(host, sslMode, password string) {
	isLocal := host == "localhost" || host == "127.0.0.1" || host == "::1"

	if !isLocal && sslMode == "disable" {
		log.Printf("WARNING: connecting to database host %q with DB_SSLMODE=disable -- unencrypted connection", host)
	}
	if !isLocal && password == knownDevPassword {
		log.Printf("WARNING: DB_PASSWORD matches default development password against host %q", host)
	}
}

func bootstrapAPIKey(ctx context.Context, keyRepo *auth.KeyRepository) error {
	count, err := keyRepo.Count(ctx)
	if err != nil {
		return fmt.Errorf("failed to check existing api keys: %w", err)
	}
	if count > 0 {
		return nil
	}

	if adminKey := os.Getenv("ADMIN_API_KEY"); adminKey != "" {
		id := ulid.MustNew(ulid.Timestamp(time.Now()), ulid.Monotonic(cryptorand.Reader, 0)).String()
		if err := keyRepo.Create(ctx, id, auth.HashKey(adminKey), "admin-bootstrap (from ADMIN_API_KEY)"); err != nil {
			return err
		}
		log.Println("Registered API key from ADMIN_API_KEY environment variable")
		return nil
	}

	generated, err := auth.GenerateAPIKey()
	if err != nil {
		return fmt.Errorf("failed to generate bootstrap api key: %w", err)
	}
	if err := keyRepo.Create(ctx, generated.ID, generated.Hash, "admin-bootstrap (generated)"); err != nil {
		return err
	}
	log.Println("=======================================================================")
	log.Println("Generated initial API key:")
	log.Printf("  %s", generated.Plaintext)
	log.Println("Pass this key as: Authorization: Bearer <key>")
	log.Println("=======================================================================")
	return nil
}
