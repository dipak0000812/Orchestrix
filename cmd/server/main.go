package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/dipak0000812/orchestrix/internal/config"
)

func main() {
	var configPath string

	flag.StringVar(&configPath, "config", "configs/base.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	fmt.Println("Orchestrix starting")
	fmt.Printf("Server port: %d\n", cfg.Server.Port)
	fmt.Printf("Log level: %s (%s)\n", cfg.Logging.Level, cfg.Logging.Format)
	fmt.Printf("Shutdown timeout: %s\n", cfg.Shutdown.Timeout)

	os.Exit(0)
}
