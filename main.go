// Command tandem is a self-hosted personal assistant: contacts, reminders,
// interactions and notes, managed by chatting with Claude. This file only
// wires things together; the app lives in internal/ (see README).
package main

import (
	"flag"
	"log"
	"net/http"
	"path/filepath"

	"github.com/jwald3/tandem/internal/config"
	"github.com/jwald3/tandem/internal/seed"
	"github.com/jwald3/tandem/internal/server"
	"github.com/jwald3/tandem/internal/store"
)

func main() {
	seedDemo := flag.Bool("seed-demo", false, "fill an empty database with demo contacts, reminders, interactions and notes (refuses if it has data)")
	dbFlag := flag.String("db", "", "SQLite database file (overrides DB_PATH; default tandem.db)")
	flag.Parse()

	config.LoadDotEnv(".env")
	cfg := config.FromEnv()
	if *dbFlag != "" {
		cfg.DBPath = *dbFlag
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if *seedDemo {
		if err := seed.Demo(st); err != nil {
			log.Fatalf("seed demo: %v", err)
		}
		log.Printf("seeded demo data into %s", filepath.Clean(cfg.DBPath))
	}

	app, err := server.New(st, cfg.AnthropicBaseURL)
	if err != nil {
		log.Fatalf("parse templates: %v", err)
	}
	app.InitAPIKey(cfg.APIKey)

	log.Printf("Tandem listening on http://%s (db: %s)", cfg.Addr, filepath.Clean(cfg.DBPath))
	if err := http.ListenAndServe(cfg.Addr, app.Handler()); err != nil {
		log.Fatal(err)
	}
}
