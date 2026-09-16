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
		authFlag    = flag.Bool("auth", false, "Force enable authentication (requires user login)")
		noAuthFlag  = flag.Bool("no-auth", false, "Disable authentication (single-user mode)")
		apiKeyFlag  = flag.String("api-key", "", "API key / PAT for MCP server authentication")
		tokenFlag   = flag.String("token", "", "Bearer/Session token for MCP server authentication")
		userFlag    = flag.String("user", "", "User ID or username for MCP server in local mode")
		mcpFlag     = flag.Bool("mcp", false, "Start Model Context Protocol (MCP) server over stdio")
		versionFlag = flag.Bool("version", false, "Print version and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s\nVersion: %s\n\nUsage:\n  remgo [flags] [command]\n\nCommands:\n  serve        Start the HTTP web server (default)\n  mcp          Start the MCP stdio server for AI agents\n  create-user  Create a user account from CLI\n  create-pat   Create a Personal Access Token from CLI\n  version      Print version information\n\nFlags:\n", Banner, Version)
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	if *mcpFlag || cmd == "mcp" {
		cmd = "mcp"
	}

	if *versionFlag || cmd == "version" {
		fmt.Printf("RemGo v%s\n", Version)
		os.Exit(0)
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
	if *apiKeyFlag == "" {
		*apiKeyFlag = os.Getenv("REMGO_API_KEY")
	}
	if *tokenFlag == "" {
		*tokenFlag = os.Getenv("REMGO_TOKEN")
	}
	if *userFlag == "" {
		*userFlag = os.Getenv("REMGO_USER")
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

	// Auth mode determination
	authEnabled := false
	if *noAuthFlag || os.Getenv("REMGO_NO_AUTH") == "true" || os.Getenv("REMGO_NO_AUTH") == "1" {
		authEnabled = false
	} else if *authFlag || os.Getenv("REMGO_AUTH") == "true" || os.Getenv("REMGO_AUTH") == "1" {
		authEnabled = true
	} else {
		// Auto mode: enabled if users exist in the database
		hasUsers, _ := database.HasUsers()
		authEnabled = hasUsers
	}

	// Subcommand: create-user
	if cmd == "create-user" {
		userCmd := flag.NewFlagSet("create-user", flag.ExitOnError)
		uName := userCmd.String("username", "", "Username")
		uEmail := userCmd.String("email", "", "Email address (optional)")
		uPass := userCmd.String("password", "", "Password")
		uRole := userCmd.String("role", "", "Role ('admin' or 'user')")
		_ = userCmd.Parse(args[1:])

		if *uName == "" || *uPass == "" {
			fmt.Println("Usage: remgo create-user -username <username> -password <password> [-email <email>] [-role <admin|user>]")
			os.Exit(1)
		}
		if *uEmail == "" {
			*uEmail = *uName + "@remgo.local"
		}

		u, err := database.CreateUser(*uName, *uEmail, *uPass, *uRole)
		if err != nil {
			log.Fatalf("Failed to create user: %v", err)
		}
		fmt.Printf("User created successfully!\n  ID:       %s\n  Username: %s\n  Email:    %s\n  Role:     %s\n", u.ID, u.Username, u.Email, u.Role)
		return
	}

	// Subcommand: create-pat
	if cmd == "create-pat" {
		patCmd := flag.NewFlagSet("create-pat", flag.ExitOnError)
		targetUser := patCmd.String("user", "", "Username or User ID")
		patName := patCmd.String("name", "CLI Access Token", "Token name")
		_ = patCmd.Parse(args[1:])

		if *targetUser == "" {
			fmt.Println("Usage: remgo create-pat -user <username_or_id> [-name <token_name>]")
			os.Exit(1)
		}

		u, err := database.GetUserByUsernameOrEmail(*targetUser)
		if err != nil || u == nil {
			u, err = database.GetUserByID(*targetUser)
		}
		if err != nil || u == nil {
			log.Fatalf("User '%s' not found", *targetUser)
		}

		rawKey, keyRec, err := database.CreateAPIKey(u.ID, *patName, nil)
		if err != nil {
			log.Fatalf("Failed to create PAT: %v", err)
		}
		fmt.Printf("Personal Access Token created successfully!\n  Name:  %s\n  User:  %s (%s)\n  Key:   %s\n\nStore this key safely! It will not be displayed again.\n", keyRec.Name, u.Username, u.ID, rawKey)
		return
	}

	// Handle MCP Stdio Mode
	if cmd == "mcp" {
		var mcpUserID string

		if *apiKeyFlag != "" {
			user, _, err := database.ValidateAPIKey(*apiKeyFlag)
			if err != nil || user == nil {
				log.Fatalf("MCP authentication failed: invalid API key")
			}
			mcpUserID = user.ID
		} else if *tokenFlag != "" {
			user, _, err := database.ValidateSession(*tokenFlag)
			if err != nil || user == nil {
				log.Fatalf("MCP authentication failed: invalid token")
			}
			mcpUserID = user.ID
		} else if *userFlag != "" {
			user, err := database.GetUserByUsernameOrEmail(*userFlag)
			if err != nil || user == nil {
				user, err = database.GetUserByID(*userFlag)
			}
			if err != nil || user == nil {
				log.Fatalf("MCP user '%s' not found", *userFlag)
			}
			mcpUserID = user.ID
		} else if authEnabled {
			fmt.Fprintln(os.Stderr, "Error: Authentication is enabled for this RemGo database.")
			fmt.Fprintln(os.Stderr, "Please provide an API key via --api-key flag or REMGO_API_KEY environment variable.")
			os.Exit(1)
		} else {
			mcpUserID = db.DefaultUserID
		}

		mcpServer := mcp.NewServer(database)
		if err := mcpServer.ServeStdio(mcpUserID); err != nil {
			log.Fatalf("MCP server exited with error: %v", err)
		}
		return
	}

	// Default: Start HTTP Outliner & SRS Web Server
	staticHandler := web.Handler()
	apiServer := api.NewServer(database, staticHandler, authEnabled)

	addr := fmt.Sprintf("%s:%d", *hostFlag, *portFlag)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      apiServer.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Print Startup Banner
	authStatusStr := "Disabled (single-user mode)"
	if authEnabled {
		hasUsers, _ := database.HasUsers()
		if hasUsers {
			authStatusStr = "Enabled (multi-user protected)"
		} else {
			authStatusStr = "Enabled (pending first admin setup)"
		}
	}

	fmt.Print(Banner)
	fmt.Printf("  • Version:     %s\n", Version)
	fmt.Printf("  • Web UI:      http://%s\n", addr)
	fmt.Printf("  • Database:    %s\n", dbPath)
	fmt.Printf("  • Auth Mode:   %s\n", authStatusStr)
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
	tree, err := d.GetTree(db.DefaultUserID, nil)
	if err == nil && len(tree) == 0 {
		doc, err := d.CreateRem(db.DefaultUserID, nil, "Getting Started with RemGo", nil)
		if err != nil {
			return
		}
		d.CreateRem(db.DefaultUserID, &doc.ID, "RemGo is a local-first, zero-latency outliner with FSRS spaced repetition.", nil)
		d.CreateRem(db.DefaultUserID, &doc.ID, "Forward Card :: Front asks question, Back contains answer", nil)
		d.CreateRem(db.DefaultUserID, &doc.ID, "Two-Way Card ::: Creates two bidirectional flashcards", nil)
		d.CreateRem(db.DefaultUserID, &doc.ID, "Mitochondria ;; Powerhouse of the cell (concept/descriptor card)", nil)
		d.CreateRem(db.DefaultUserID, &doc.ID, "Fill in the blank: The speed of light is {{299,792,458}} m/s", nil)
		listRem, err := d.CreateRem(db.DefaultUserID, &doc.ID, "Primary colors ==>", nil)
		if err == nil {
			d.CreateRem(db.DefaultUserID, &listRem.ID, "Red", nil)
			d.CreateRem(db.DefaultUserID, &listRem.ID, "Green", nil)
			d.CreateRem(db.DefaultUserID, &listRem.ID, "Blue", nil)
		}
		d.CreateRem(db.DefaultUserID, &doc.ID, "Link topics using [[references]] to build an interconnected knowledge graph.", nil)
	}
}
