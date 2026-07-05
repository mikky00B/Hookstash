package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hookstash/hookstash/internal/config"
	"github.com/hookstash/hookstash/internal/server"
	"github.com/hookstash/hookstash/internal/store"
)

func main() {
	cfg := config.Default()

	flag.StringVar(&cfg.Host, "host", cfg.Host, "host to bind")
	flag.IntVar(&cfg.Port, "port", cfg.Port, "port to listen on")
	flag.StringVar(&cfg.ForwardURL, "forward", cfg.ForwardURL, "optional local webhook target URL")
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "SQLite database path")
	flag.BoolVar(&cfg.OpenBrowser, "open", cfg.OpenBrowser, "open dashboard in the default browser")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	flag.Parse()

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	handler := server.New(server.Config{
		Store:      db,
		ForwardURL: cfg.ForwardURL,
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	printStartup(cfg)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown server: %v", err)
	}
}

func printStartup(cfg config.Config) {
	baseURL := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)
	forward := cfg.ForwardURL
	if forward == "" {
		forward = "not configured"
	}

	fmt.Println("Hookstash is running.")
	fmt.Println()
	fmt.Printf("Dashboard:       %s\n", baseURL)
	fmt.Printf("Webhook URL:     %s/hooks/default\n", baseURL)
	fmt.Printf("Forward target:  %s\n", forward)
	fmt.Printf("Database:        %s\n", cfg.DBPath)
	if cfg.Host == "0.0.0.0" {
		fmt.Println()
		fmt.Println("Warning: Hookstash is listening on 0.0.0.0. Your dashboard may be reachable from other devices on your network.")
	}
}
