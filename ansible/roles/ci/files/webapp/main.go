// Reference workload for the lab's CI/CD pipeline (docs/PLAN.md Phase 7): one replica per region, one PostgreSQL
// in the control plane's region. Each request records a visit and reports where it was served, which version
// answered and what the database round trip cost from that region, so the WAN shows in every response.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// Set at build time: -ldflags "-X main.version=<commit>"
var version = "dev"

func main() {
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	node, region := os.Getenv("NODE_NAME"), os.Getenv("REGION")
	// Created on the first request that reaches the database, so a replica starts before PostgreSQL is up
	var schema sync.Mutex
	ready := false
	ensureSchema := func() error {
		schema.Lock()
		defer schema.Unlock()
		if ready {
			return nil
		}
		_, err := db.Exec("CREATE TABLE IF NOT EXISTS visits (at timestamptz NOT NULL DEFAULT now(), region text)")
		ready = err == nil
		return err
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var visits int64
		err := ensureSchema()
		// One round trip to the database, timed: from another region it costs at least the link's RTT
		began := time.Now()
		if err == nil {
			err = db.QueryRow("INSERT INTO visits (region) VALUES ($1) RETURNING (SELECT count(*) + 1 FROM visits)",
				region).Scan(&visits)
		}
		took := time.Since(began)
		w.Header().Set("Content-Type", "text/plain")
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprintf(w, "Version: %s\nNode: %s\nRegion: %s\n", version, node, region)
		if err != nil {
			fmt.Fprintf(w, "Database: %v\n", err)
			return
		}
		fmt.Fprintf(w, "Visits: %d\nDatabase: %d ms\n", visits, took.Milliseconds())
	})
	log.Printf("webapp %s on %s (region %s) listening on :8080", version, node, region)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
