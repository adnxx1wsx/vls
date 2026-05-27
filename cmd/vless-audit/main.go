package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"vless-audit/internal/api"
	"vless-audit/internal/collector"
	"vless-audit/internal/config"
	"vless-audit/internal/store"
	"vless-audit/static"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	// Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Open database.
	db, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Run periodic cleanup.
	go func() {
		db.PurgeOld(cfg.RetentionDays)
	}()

	// Start access log collector.
	logCollector := collector.NewLogCollector(db, cfg.AccessLog)
	if err := logCollector.Start(); err != nil {
		log.Printf("[main] log collector failed to start: %v (continuing without log tail)", err)
	}
	defer logCollector.Stop()

	// Start stats collector (only if API address is configured).
	if cfg.XrayAPI != "" && cfg.PollIntervalSec > 0 {
		xrayBin := cfg.XrayBinPath
		if xrayBin == "" {
			xrayBin = "xray" // fallback to PATH
		}
		statsCollector := collector.NewStatsCollector(db, xrayBin, cfg.XrayAPI, cfg.PollIntervalSec)
		if err := statsCollector.Start(); err != nil {
			log.Printf("[main] stats collector failed to start: %v (continuing without stats poll)", err)
		}
		defer statsCollector.Stop()
	} else {
		log.Println("[main] stats collector disabled (no xray_api configured)")
	}

	// Prepare embedded static files.
	staticFS, err := fs.Sub(static.FS, "web")
	if err != nil {
		log.Fatalf("failed to load embedded web: %v", err)
	}

	// Build router with auth + Xray sync.
	router := api.NewRouter(db, logCollector.ConnChannel(), http.FS(staticFS), cfg.AuthToken, cfg.XrayConfigPath, cfg.XrayBinPath, cfg.RegisterSecret, cfg.AccessLog)
	if cfg.AuthToken != "" {
		fmt.Printf("🔑 登录密码: %s\n", cfg.AuthToken)
	}

	// Start HTTP server.
	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: router,
	}

	go func() {
		fmt.Printf("vless-audit listening on http://%s\n", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for shutdown signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	srv.Close()
}
