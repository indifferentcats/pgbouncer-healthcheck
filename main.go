package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/julienschmidt/httprouter"
	_ "github.com/lib/pq"
)

var (
	config configuration
	db     *sql.DB
)

func initServer() *http.Server {
	router := httprouter.New()

	// Add a default root 200 handler
	router.GET("/", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {})

	// Add a version endpoint
	router.GET("/ami-version", makeRequestHandlerFile("version info", "/etc/ami_version"))

	addHealthHandlers(router)
	addStatusHandlers(router)
	if config.EnableDebugEndpoints {
		log.Print("Enabling Debug Endpoints")
		addDebugHandlers(router)
	}
	// Handlers time out after requestTimeout (10s), so WriteTimeout must be
	// longer than that.
	return &http.Server{
		Handler:           router,
		Addr:              fmt.Sprintf(":%d", config.Port),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func initDB() {
	var err error
	if probeDB, err = openProbeDB(config.Connstr); err != nil {
		probeDB = nil
	}
	if config.BackendConnstr != "" {
		if backendDB, err = openProbeDB(config.BackendConnstr); err != nil {
			backendDB = nil
			log.Printf("Could not open backend database: %s", err)
		}
	}

	db, err = sql.Open("postgres", config.Connstr)
	if err != nil {
		db = nil
		log.Printf("Could not open database: %s", err)
		return
	}
	// sql.Open does not connect; run a query so the log reflects reality.
	// db.Ping sends ";", which the PGBouncer admin console may not accept.
	// A failure here is not fatal: PGBouncer may not be up yet, and the pool
	// reconnects on demand.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, "SHOW VERSION")
	if err != nil {
		log.Printf("Could not connect to PGBouncer database (will retry on request): %s", err)
		return
	}
	rows.Close()
	log.Printf("Connected to PGBouncer database")
}

func version(w io.Writer) {
	fmt.Fprintf(w, "%s version %s\n", os.Args[0], VERSION)
	fmt.Fprintf(w, "built %s\n", BUILD_DATE)
}

func main() {
	var err error
	config, err = loadConfig(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(0)
	}
	if err != nil {
		log.Fatalf("Invalid configuration: %s", err)
	}
	initDB()
	webserver := initServer()
	log.Printf("Listening on port %d", config.Port)
	if err := webserver.ListenAndServe(); err != nil {
		log.Fatalf("Error occurred while listening for connections: %s", err)
	}
}
