package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"

	"strings"
	"syscall"
	"time"

	"github.com/hookstash/hookstash/internal/config"
	"github.com/hookstash/hookstash/internal/server"
	"github.com/hookstash/hookstash/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "endpoint" {
		runEndpointCommand(os.Args[2:])
		return
	}

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

func runEndpointCommand(args []string) {
	if len(args) == 0 {
		endpointUsage()
		os.Exit(2)
	}

	cfg := config.Default()
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	switch args[0] {
	case "add":
		flags := flag.NewFlagSet("endpoint add", flag.ExitOnError)
		withToken := flags.Bool("token", false, "require a capture token for this endpoint")
		provider := flags.String("provider", "", "provider hint for the Signature Lab (e.g. stripe, github)")

		// Pull the positional endpoint name out first so flags may appear
		// before or after it (`add payments --token` and `add --token payments`).
		var positional []string
		var flagArgs []string
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			arg := rest[i]
			switch {
			case arg == "--token":
				flagArgs = append(flagArgs, arg)
			case arg == "--provider" && i+1 < len(rest):
				flagArgs = append(flagArgs, arg, rest[i+1])
				i++
			case strings.HasPrefix(arg, "--provider="):
				flagArgs = append(flagArgs, arg)
			default:
				positional = append(positional, arg)
			}
		}
		if err := flags.Parse(flagArgs); err != nil || len(positional) != 1 {
			endpointUsage()
			os.Exit(2)
		}
		endpoint, token, err := createEndpoint(ctx, db, positional[0], *provider, *withToken)
		if err != nil {
			log.Fatalf("create endpoint: %v", err)
		}
		fmt.Printf("Endpoint created: /hooks/%s\n", endpoint.Slug)
		fmt.Printf("ID:       %s\n", endpoint.ID)
		if token != "" {
			fmt.Printf("Token:    %s\n", token)
			fmt.Println("(shown once; capture requires 'Authorization: Bearer <token>' or ?token=)")
		}
	case "list":
		endpoints, err := db.ListEndpoints(ctx)
		if err != nil {
			log.Fatalf("list endpoints: %v", err)
		}
		if len(endpoints) == 0 {
			fmt.Println("No endpoints yet.")
			return
		}
		for _, endpoint := range endpoints {
			auth := "open"
			if endpoint.TokenHash != "" {
				auth = "token required"
			}
			provider := endpoint.Provider
			if provider == "" {
				provider = "-"
			}
			fmt.Printf("/hooks/%-20s %-10s %s\n", endpoint.Slug, provider, auth)
		}
	case "rm":
		if len(args) < 2 {
			endpointUsage()
			os.Exit(2)
		}
		endpoint, err := db.GetEndpointBySlug(ctx, args[1])
		if err != nil {
			log.Fatalf("find endpoint: %v", err)
		}
		if endpoint.ID == "ep_default" {
			log.Fatal("the default endpoint cannot be deleted")
		}
		if err := db.DeleteEndpoint(ctx, endpoint.ID); err != nil {
			log.Fatalf("delete endpoint: %v", err)
		}
		fmt.Printf("Endpoint deleted: /hooks/%s\n", endpoint.Slug)
	default:
		endpointUsage()
		os.Exit(2)
	}
}

func createEndpoint(ctx context.Context, db *store.SQLiteStore, slug, provider string, withToken bool) (store.Endpoint, string, error) {
	slug, err := store.NormalizeSlug(slug)
	if err != nil {
		return store.Endpoint{}, "", err
	}
	if _, err := db.GetEndpointBySlug(ctx, slug); err == nil {
		return store.Endpoint{}, "", fmt.Errorf("endpoint %q already exists", slug)
	}

	endpoint := store.Endpoint{
		ID:        store.NewEndpointID(),
		Slug:      slug,
		Provider:  strings.TrimSpace(provider),
		CreatedAt: time.Now().UTC(),
	}
	token := ""
	if withToken {
		generated, tokenHash, err := store.GenerateToken()
		if err != nil {
			return store.Endpoint{}, "", err
		}
		endpoint.TokenHash = tokenHash
		token = generated
	}
	if err := db.CreateEndpoint(ctx, endpoint); err != nil {
		return store.Endpoint{}, "", err
	}
	return endpoint, token, nil
}

func endpointUsage() {
	fmt.Println("Usage: hookstash endpoint <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  add <name> [--token] [--provider <name>]   create a capture endpoint")
	fmt.Println("  list                                       list capture endpoints")
	fmt.Println("  rm <name>                                  delete a capture endpoint")
}
