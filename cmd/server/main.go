package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv" // ← Add this
	"syscall"
	"time"

	"github.com/dipak0000812/orchestrix/internal/api"
	"github.com/dipak0000812/orchestrix/internal/executor"
	"github.com/dipak0000812/orchestrix/internal/job/model"
	"github.com/dipak0000812/orchestrix/internal/job/repository"
	"github.com/dipak0000812/orchestrix/internal/job/service"
	"github.com/dipak0000812/orchestrix/internal/job/state"
	"github.com/dipak0000812/orchestrix/internal/scheduler"
	"github.com/dipak0000812/orchestrix/internal/worker"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	log.Println("Starting Orchestrix...")

	// 1. Create database connection
	dbConfig := repository.DBConfig{
		Host:            getEnv("DB_HOST", "localhost"),
		Port:            getEnvInt("DB_PORT", 5434),
		User:            getEnv("DB_USER", "orchestrix"),
		Password:        getEnv("DB_PASSWORD", "orchestrix_dev_password"),
		Database:        getEnv("DB_NAME", "orchestrix_dev"),
		SSLMode:         getEnv("DB_SSLMODE", "disable"),
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

	// 2. Create repository and service
	repo := repository.NewPostgresJobRepository(pool)
	stateMachine := state.NewStateMachine()
	idGen := service.NewULIDGenerator()
	retryConfig := service.DefaultRetryConfig()
	jobService := service.NewJobService(repo, stateMachine, idGen, retryConfig)

	// 3. Create executor registry
	executors := executor.NewExecutorRegistry()
	executors.Register("demo_job", executor.NewDemoExecutor(1*time.Second))
	log.Println("Registered executors: demo_job")

	// 4. Create job channel
	jobChannel := make(chan *model.Job, 100)

	// 5. Create and start scheduler
	sched := scheduler.NewScheduler(
		jobService,
		1*time.Second, // Poll interval
		10,            // Batch size
		jobChannel,
	)
	sched.Start()
	defer sched.Stop()

	// 6. Create and start worker pool
	workers := worker.NewWorkerPool(
		5, // Number of workers
		jobChannel,
		executors,
		jobService,
		10*time.Second, // Job timeout
	)
	workers.Start()
	defer workers.Stop()

	// 7. Create HTTP handler and router
	// 7. Create metrics and HTTP handler
	metrics := api.NewMetrics()
	handler := api.NewHandler(jobService, metrics) // ← Pass metrics

	router := http.NewServeMux()
	router.HandleFunc("POST /api/v1/jobs", handler.CreateJob)
	router.HandleFunc("GET /api/v1/jobs/{id}", handler.GetJob)
	router.HandleFunc("GET /api/v1/jobs", handler.ListJobs)
	router.HandleFunc("DELETE /api/v1/jobs/{id}", handler.CancelJob)
	router.HandleFunc("GET /health", handler.Health)
	router.Handle("GET /metrics", promhttp.Handler()) // ← Add this

	// 8. Create HTTP server
	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// 9. Start HTTP server in goroutine
	go func() {
		log.Println("HTTP server listening on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// 10. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")

	// 11. Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}

// getEnv gets environment variable or returns default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets environment variable as int or returns default.
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}
