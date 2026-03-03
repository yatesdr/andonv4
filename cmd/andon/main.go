package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"andon/server"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ensureCert generates a self-signed ECDSA certificate in dir if not already present.
// Returns paths to cert.pem and key.pem.
func ensureCert(dir string) (string, string) {
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return certPath, keyPath
		}
	}

	log.Println("Generating self-signed TLS certificate...")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate TLS key: %v", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		log.Fatalf("Failed to generate serial number: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"Andon Canvas"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),

		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,

		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:    []string{"localhost"},
	}

	// Add local hostname as SAN
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, hostname)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("Failed to create certificate: %v", err)
	}

	os.MkdirAll(dir, 0755)

	certFile, err := os.Create(certPath)
	if err != nil {
		log.Fatalf("Failed to write cert.pem: %v", err)
	}
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certFile.Close()

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Fatalf("Failed to marshal private key: %v", err)
	}
	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Failed to write key.pem: %v", err)
	}
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyFile.Close()

	log.Printf("TLS certificate written to %s", dir)
	return certPath, keyPath
}

// cacheStatic wraps a handler with Cache-Control headers for static assets.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}

func runRestore(configPath string, args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	source := fs.String("source", "", "backup source: s3 or central")
	station := fs.String("station", "", "station identifier (UUID, prefix, or partial UUID)")
	s3Endpoint := fs.String("s3-endpoint", "", "S3 endpoint")
	s3Bucket := fs.String("s3-bucket", "", "S3 bucket")
	s3AccessKey := fs.String("s3-access-key", "", "S3 access key")
	s3SecretKey := fs.String("s3-secret-key", "", "S3 secret key")
	s3SSL := fs.Bool("s3-ssl", false, "use SSL for S3")
	s3Region := fs.String("s3-region", "", "S3 region")
	centralURL := fs.String("central-url", "", "central server URL")
	fs.Parse(args)

	if *source == "" {
		fmt.Fprintln(os.Stderr, "Usage: andon restore --source <s3|central> --station <id> [options]")
		fs.PrintDefaults()
		os.Exit(1)
	}

	// Determine data directory
	dataDir := filepath.Dir(configPath)
	os.MkdirAll(dataDir, 0755)

	switch *source {
	case "s3":
		if *station == "" || *s3Endpoint == "" || *s3Bucket == "" {
			fmt.Fprintln(os.Stderr, "S3 restore requires --station, --s3-endpoint, and --s3-bucket")
			os.Exit(1)
		}
		bs := server.BackupSettings{
			S3Endpoint: *s3Endpoint,
			S3Bucket:   *s3Bucket,
			S3AccessKey: *s3AccessKey,
			S3SecretKey: *s3SecretKey,
			S3UseSSL:   *s3SSL,
			S3Region:   *s3Region,
		}
		if err := server.RestoreFromS3(dataDir, bs, *station); err != nil {
			log.Fatalf("Restore failed: %v", err)
		}
	case "central":
		if *station == "" || *centralURL == "" {
			fmt.Fprintln(os.Stderr, "Central restore requires --station and --central-url")
			os.Exit(1)
		}
		if err := server.RestoreFromCentral(dataDir, *centralURL, *station); err != nil {
			log.Fatalf("Restore failed: %v", err)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown source: %s (use s3 or central)\n", *source)
		os.Exit(1)
	}

	fmt.Println("Restore complete. Start the server to verify.")
}

func main() {
	port := flag.String("port", "8090", "server port")
	configPath := flag.String("config", "", "path to config file (default ~/.andon/config.json)")
	flag.Parse()

	// Check for subcommands
	if args := flag.Args(); len(args) > 0 && args[0] == "restore" {
		cp := *configPath
		if cp == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				log.Fatal(err)
			}
			cp = filepath.Join(home, ".andon", "config.json")
		}
		runRestore(cp, args[1:])
		return
	}

	if p := os.Getenv("PORT"); p != "" {
		*port = p
	}

	// Find project root (where data/, templates/, static/ live)
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// Initialize store
	if *configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		*configPath = filepath.Join(home, ".andon", "config.json")
	}
	andonDir := filepath.Dir(*configPath)
	os.MkdirAll(andonDir, 0755)
	store, err := server.NewStore(*configPath)
	if err != nil {
		log.Fatalf("Failed to load store: %v", err)
	}

	// Auto-TLS certificate
	certPath, keyPath := ensureCert(andonDir)

	// Parse templates
	htmlTmpl, err := template.ParseGlob(filepath.Join(root, "templates", "*.html"))
	if err != nil {
		log.Fatalf("Failed to parse templates: %v", err)
	}

	// Cancellable context for background goroutines
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// SSE hub
	hub := server.NewHub(store)
	hub.LoadScreenState()

	// Event logger (SQLite)
	dbPath := filepath.Join(andonDir, "andon.db")
	eventLog, err := server.NewEventLogger(dbPath, store, hub)
	if err != nil {
		log.Fatalf("Failed to init event logger: %v", err)
	}
	go eventLog.Run(ctx)

	// Wire event logger into hub for count session hooks
	hub.SetEventLog(eventLog)

	// Warlink PLC bridge client
	warlinkClient := server.NewWarlinkClient(store, hub, eventLog)
	hub.SetWarlink(warlinkClient)
	go warlinkClient.Run(ctx)

	// Start count sessions for screens that are already active
	eventLog.InitActiveSessions()

	// Auto-start/stop screens based on shift schedule
	go hub.RunAutoStart(ctx)

	// Backup manager
	backup := server.NewBackupManager(store, dbPath)
	store.SetOnSave(backup.NotifyConfigChange)
	go backup.Run(ctx)

	// Auth
	auth := server.NewAuth(store, htmlTmpl)
	auth.StartCleanup(ctx)

	// Dev tools: simulator and data generator
	simulator := server.NewSimulator(warlinkClient, store, hub, eventLog)
	dataGen := server.NewDataGenerator(eventLog, store)

	// Handlers
	handlers := &server.Handlers{
		Store:     store,
		Templates: htmlTmpl,
		Auth:      auth,
		Hub:       hub,
	}
	api := &server.API{
		Store:     store,
		Hub:       hub,
		Auth:      auth,
		Warlink:   warlinkClient,
		EventLog:  eventLog,
		Backup:    backup,
		Simulator: simulator,
		DataGen:   dataGen,
	}

	r := chi.NewRouter()

	// SSE — no compression (must stream unbuffered)
	r.Get("/events/_dashboard", hub.ServeDashboardSSE)
	r.Get("/events/{slug}", hub.ServeSSE)

	// Everything else gets gzip compression
	r.Group(func(r chi.Router) {
		r.Use(middleware.Compress(5))

		// Auth
		r.Get("/login", auth.LoginPage)
		r.Post("/login", auth.LoginSubmit)
		r.Post("/logout", auth.Logout)

		// Pages
		r.Get("/", handlers.Dashboard)
		r.Get("/screens/{slug}", handlers.Display)
		r.Get("/counts", handlers.Counts)
		r.Get("/reports", handlers.Reports)
		r.Get("/workorders", handlers.WorkOrders)
		r.Get("/settings", handlers.Settings)

		// Auth-protected pages
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuthMiddleware)
			r.Get("/designer", handlers.Designer)
			r.Get("/configure/{id}", handlers.Configure)
			r.Get("/visual-mappings", handlers.VisualMappings)
			r.Get("/devtools", handlers.DevTools)
		})

		// API
		r.Route("/api", func(r chi.Router) {
			// Screens
			r.Get("/screens", api.ListScreens)
			r.Post("/screens", auth.RequireAuth(api.CreateScreen))

			r.Route("/screens/{id}", func(r chi.Router) {
				r.Get("/", api.GetScreen)
				r.Put("/", auth.RequireAuth(api.UpdateScreen))
				r.Delete("/", auth.RequireAuth(api.DeleteScreen))
				r.Get("/layout", api.GetLayout)
				r.Put("/layout", auth.RequireAuth(api.SaveLayout))
				r.Get("/config", api.GetCellConfig)
				r.Put("/config", auth.RequireAuth(api.SaveCellConfig))
				r.Put("/overlay", api.ToggleScreenOverlay)
				r.Put("/active", api.ToggleScreenActive)
				r.Put("/auto-start", api.ToggleAutoStart)
			})

			// Global state
			r.Get("/global", api.GetGlobal)
			r.Put("/global", api.SetGlobal)

			// Reporting units
			r.Get("/reporting-units", api.GetReportingUnits)
			r.Put("/reporting-units", auth.RequireAuth(api.SetReportingUnits))

			// Visual mappings
			r.Get("/visual-mappings", api.GetVisualMappings)
			r.Put("/visual-mappings", auth.RequireAuth(api.SetVisualMappings))

			// Settings (auth-protected)
			r.Get("/settings", auth.RequireAuth(api.GetSettings))
			r.Put("/settings", auth.RequireAuth(api.SetSettings))

			// Hourly counts
			r.Get("/hourly-counts", api.GetHourlyCounts)

			// Reports
			r.Get("/reports", api.GetReports)

			// Shift summary
			r.Get("/shift-summary", api.GetShiftSummary)
			r.Post("/shift-summary/recompute", auth.RequireAuth(api.RecomputeShiftSummary))

			// Export
			r.Get("/export/hourly-counts.csv", api.ExportHourlyCountsCSV)
			r.Get("/export/hourly-counts.xlsx", api.ExportHourlyCountsXLSX)
			r.Get("/export/shift-summary.csv", api.ExportShiftSummaryCSV)
			r.Get("/export/shift-summary.xlsx", api.ExportShiftSummaryXLSX)
			r.Get("/export/reports.csv", api.ExportReportsCSV)
			r.Get("/export/reports.xlsx", api.ExportReportsXLSX)

			// Event log
			r.Get("/eventlog/status", api.GetEventLogStatus)
			r.Post("/eventlog/prune", auth.RequireAuth(api.PruneEventLog))

			// Work orders (public)
			r.Post("/workorders", api.SubmitWorkOrder)

			// E-Maintenance
			r.Post("/emaint/test", auth.RequireAuth(api.TestEMaint))

			// Warlink
			r.Post("/warlink/test", auth.RequireAuth(api.TestWarlink))
			r.Get("/warlink/plcs", api.WarlinkPlcs)
			r.Get("/warlink/tags/{plc}", api.WarlinkTags)

			// Backup
			r.Get("/backup/status", auth.RequireAuth(api.GetBackupStatus))
			r.Post("/backup/trigger", auth.RequireAuth(api.TriggerBackup))

			// Auth
			r.Put("/auth/password", auth.RequireAuth(api.ChangePassword))

			// Dev tools (auth-protected)
			r.Route("/devtools", func(r chi.Router) {
				r.Post("/sim/start", auth.RequireAuth(api.SimStart))
				r.Post("/sim/stop", auth.RequireAuth(api.SimStop))
				r.Get("/sim/status", auth.RequireAuth(api.SimStatus))
				r.Post("/generate", auth.RequireAuth(api.DataGenGenerate))
				r.Get("/generate/progress", auth.RequireAuth(api.DataGenProgress))
				r.Post("/verify", auth.RequireAuth(api.DataGenVerify))
				r.Post("/clear", auth.RequireAuth(api.DataGenClear))
			})
		})

		// Static files with cache headers
		staticDir := filepath.Join(root, "static")
		r.Get("/static/*", http.StripPrefix("/static/", cacheStatic(http.FileServer(http.Dir(staticDir)))).ServeHTTP)
	})

	addr := ":" + *port
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		<-ctx.Done()
		log.Println("Shutting down server...")
		srv.Shutdown(context.Background())
	}()

	// HTTP redirect listener on port+1
	portNum, _ := strconv.Atoi(*port)
	httpAddr := fmt.Sprintf(":%d", portNum+1)
	httpRedirect := &http.Server{
		Addr: httpAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := "https://" + r.Host
			// Replace redirect port with TLS port if needed
			if host, _, err := net.SplitHostPort(r.Host); err == nil {
				target = "https://" + net.JoinHostPort(host, *port)
			}
			target += r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		}),
	}
	go func() {
		log.Printf("HTTP redirect on http://localhost%s", httpAddr)
		if err := httpRedirect.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("HTTP redirect listener error: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		httpRedirect.Shutdown(context.Background())
	}()

	log.Printf("Andon server starting on https://localhost%s", addr)
	if err := srv.ListenAndServeTLS(certPath, keyPath); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
