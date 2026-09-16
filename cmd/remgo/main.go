package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/darsheee/remgo/internal/api"
	"github.com/darsheee/remgo/internal/db"
	"github.com/darsheee/remgo/internal/mcp"
	"github.com/darsheee/remgo/internal/web"
)

const Version = "1.0.0"

const Banner = `
  ____                 ____       
 |  _ \ ___ _ __ ___  / ___| ___  
 | |_) / _ \ '_ ` + "`" + ` _ \| |  _ / _ \ 
 |  _ <  __/ | | | | | |_| | (_) |
 |_| \_\___|_| |_| |_|\____|\___/ 
 RemNote-like Outliner & Spaced-Repetition Engine in Pure Go
`

func main() {
	var (
		portFlag    = flag.Int("port", 8080, "HTTP server port (or set PORT env var)")
		hostFlag    = flag.String("host", "127.0.0.1", "HTTP server host")
		dataDirFlag = flag.String("data", "./remgo_data", "Directory to store SQLite database")
		mcpFlag     = flag.Bool("mcp", false, "Start Model Context Protocol (MCP) server over stdio")
		versionFlag = flag.Bool("version", false, "Print version and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s\nVersion: %s\n\nUsage:\n  remgo [flags] [command]\n\nCommands:\n  serve    Start the HTTP web server (default)\n  mcp      Start the MCP stdio server for AI agents\n  version  Print version information\n\nFlags:\n", Banner, Version)
		flag.PrintDefaults()
	}

	flag.Parse()

	if *versionFlag {
		fmt.Printf("RemGo v%s\n", Version)
		os.Exit(0)
	}

	args := flag.Args()
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	if *mcpFlag || cmd == "mcp" {
		cmd = "mcp"
	}

	// Environment variable overrides
	if envPort := os.Getenv("PORT"); envPort != "" {
		var p int
		if _, err := fmt.Sscanf(envPort, "%d", &p); err == nil && p > 0 {
			*portFlag = p
		}
	}
	if envData := os.Getenv("REMGO_DATA"); envData != "" {
		*dataDirFlag = envData
	}

	// Database Initialization
	if err := os.MkdirAll(*dataDirFlag, 0755); err != nil {
		log.Fatalf("Failed to create data directory '%s': %v", *dataDirFlag, err)
	}
	dbPath := filepath.Join(*dataDirFlag, "remgo.db")
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Handle MCP Stdio Mode
	if cmd == "mcp" {
		mcpServer := mcp.NewServer(database)
		if err := mcpServer.ServeStdio(); err != nil {
			log.Fatalf("MCP server exited with error: %v", err)
		}
		return
	}

	// Default: Start HTTP Outliner & SRS Web Server
	staticHandler := web.Handler()
	apiServer := api.NewServer(database, staticHandler)

	addr := fmt.Sprintf("%s:%d", *hostFlag, *portFlag)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      apiServer.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Print Startup Banner
	fmt.Print(Banner)
	fmt.Printf("  • Version:     %s\n", Version)
	fmt.Printf("  • Web UI:      http://%s\n", addr)
	fmt.Printf("  • Database:    %s\n", dbPath)
	fmt.Printf("  • MCP API:     http://%s/mcp\n", addr)
	fmt.Println("  • Status:      Ready! Press Ctrl+C to stop.")
	fmt.Println()

	// Seed welcome document if database is completely empty
	seedInitialNotesIfEmpty(database)

	// Graceful shutdown channel
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	<-stopChan
	fmt.Println("\nShutting down RemGo gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	fmt.Println("RemGo stopped. Goodbye!")
}

func seedInitialNotesIfEmpty(d *db.DB) {
	tree, err := d.GetTree(nil)
	if err == nil && len(tree) == 0 {
		doc, err := d.CreateRem(nil, "Getting Started with RemGo", nil)
		if err != nil {
			return
		}
		d.CreateRem(&doc.ID, "RemGo is a local-first, zero-latency outliner with FSRS spaced repetition.", nil)
		d.CreateRem(&doc.ID, "Forward Card :: Front asks question, Back contains answer", nil)
		d.CreateRem(&doc.ID, "Two-Way Card ::: Creates two bidirectional flashcards", nil)
		d.CreateRem(&doc.ID, "Mitochondria ;; Powerhouse of the cell (concept/descriptor card)", nil)
		d.CreateRem(&doc.ID, "Fill in the blank: The speed of light is {{299,792,458}} m/s", nil)
		listRem, err := d.CreateRem(&doc.ID, "Primary colors ==>", nil)
		if err == nil {
			d.CreateRem(&listRem.ID, "Red", nil)
			d.CreateRem(&listRem.ID, "Green", nil)
			d.CreateRem(&listRem.ID, "Blue", nil)
		}
		d.CreateRem(&doc.ID, "Link topics using [[references]] to build an interconnected knowledge graph.", nil)
	}
}
