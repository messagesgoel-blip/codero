package main

// port_dashboard_cmds.go — codero dashboard and codero ports commands.
//
// These commands give operators quick access to the running dashboard URL,
// health validation, and network binding diagnostics without having to
// inspect config files or query running processes directly.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	configpkg "github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/daemon"
	dashboardpkg "github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gatecheck"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
)

// dashboardCmd implements "codero dashboard": prints the effective dashboard
// URL, optionally opens a browser, and can validate endpoint reachability.
func dashboardCmd(configPath *string) *cobra.Command {
	var (
		host             string
		port             int
		repoPath         string
		reportPath       string
		fixtureDirPath   string
		openBrws         bool
		checkMode        bool
		serveFixtureMode bool
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Show dashboard URL and status",
		Long: `Print the effective dashboard URL and optionally validate reachability.

Examples:
  codero dashboard                   # print effective URL and endpoint list
  codero dashboard --check           # validate /dashboard/, /api/v1/dashboard/overview, /gate
  codero dashboard --open            # open dashboard in default browser (interactive only)
  codero dashboard --port 9090       # override port (useful when testing non-default setups)
  codero dashboard --serve-fixture --report-path .codero/gate-check/last-report.json
  codero dashboard --serve-fixture --report-path .codero/gate-check/last-report.json --check
  codero dashboard --serve-fixture --fixture-dir scripts/evidence/fixtures/v8
  codero dashboard --serve-fixture --fixture-dir scripts/evidence/fixtures/v8 --check`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cfgErr := loadConfig(*configPath) // best-effort; fall back to defaults on error
			reportPathSet := reportPath != ""

			effectiveHost := host
			effectivePort := port
			basePath := "/dashboard"

			if cfg != nil {
				if effectiveHost == "" {
					bindHost, _, err := apiBindHostPort(cfg.APIServer.Addr)
					if err != nil {
						if cfgErr == nil {
							cfgErr = err
						}
					} else {
						effectiveHost = bindHost
					}
				}
				if effectivePort == 0 {
					_, bindPort, err := apiBindHostPort(cfg.APIServer.Addr)
					if err != nil {
						if cfgErr == nil {
							cfgErr = err
						}
					} else {
						effectivePort = bindPort
					}
				}
				if cfg.DashboardBasePath != "" {
					basePath = cfg.DashboardBasePath
				}
			}
			if effectiveHost == "" {
				effectiveHost = "localhost"
			}
			if effectivePort == 0 {
				effectivePort = configpkg.DefaultAPIServerPort
			}
			if cfgErr != nil {
				fmt.Fprintf(os.Stderr, "note: config load error (%v); using defaults where needed\n", cfgErr)
			}
			if fixtureDirPath != "" && !serveFixtureMode {
				return fmt.Errorf("--fixture-dir can only be used with --serve-fixture")
			}

			// Determine base URL: prefer explicit public URL from config.
			baseURL := dashboardBaseURL(effectiveHost, effectivePort)
			if cfg != nil && cfg.DashboardPublicBaseURL != "" {
				baseURL = strings.TrimRight(cfg.DashboardPublicBaseURL, "/")
			}

			normalizedBasePath := normalizeDashboardBasePath(basePath)
			dashURL := baseURL + normalizedBasePath + "/"
			overviewURL := baseURL + "/api/v1/dashboard/overview"
			gateChecksURL := baseURL + "/api/v1/dashboard/gate-checks"

			fmt.Printf("Dashboard URL:  %s\n", dashURL)
			fmt.Printf("Overview API:   %s\n", overviewURL)
			fmt.Printf("Gate Checks:    %s\n", gateChecksURL)
			fmt.Printf("Gate endpoint:  %s/gate\n", baseURL)

			if serveFixtureMode {
				if openBrws {
					return fmt.Errorf("--open cannot be combined with --serve-fixture")
				}
				return runDashboardFixture(effectiveHost, effectivePort, normalizedBasePath, repoPath, reportPath, fixtureDirPath, checkMode, reportPathSet)
			}

			if checkMode {
				return runDashboardCheck(baseURL, normalizedBasePath)
			}

			if openBrws {
				if !isInteractiveEnv() {
					fmt.Fprintln(os.Stderr, "note: --open skipped (non-interactive or headless environment)")
					return nil
				}
				return openBrowser(dashURL)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "daemon host (default: from config or localhost)")
	cmd.Flags().IntVar(&port, "port", 0, fmt.Sprintf("daemon port (default: from config or %d)", configpkg.DefaultAPIServerPort))
	cmd.Flags().StringVarP(&repoPath, "repo-path", "r", "", "path to repository root when serving a local fixture (default: current directory)")
	cmd.Flags().StringVar(&reportPath, "report-path", "", "gate-check report file to expose from /api/v1/dashboard/gate-checks in fixture mode")
	cmd.Flags().StringVar(&fixtureDirPath, "fixture-dir", "", "directory containing fixture data files (report.json, sessions.json, activity.json) for --serve-fixture mode")
	cmd.Flags().BoolVar(&openBrws, "open", false, "open dashboard in default browser (interactive only)")
	cmd.Flags().BoolVar(&checkMode, "check", false, "validate dashboard and API endpoints; exit non-zero on failure")
	cmd.Flags().BoolVar(&serveFixtureMode, "serve-fixture", false, "start a local dashboard fixture server backed by an empty temp state DB")

	return cmd
}

// runDashboardCheck GETs three critical endpoints and reports pass/fail.
// Returns a non-nil error if any endpoint is unreachable or returns non-2xx.
func runDashboardCheck(baseURL, basePath string) error {
	return runDashboardCheckWithOptions(baseURL, basePath, false)
}

func runDashboardCheckWithOptions(baseURL, basePath string, requireGateCheckReport bool) error {
	type endpoint struct {
		name          string
		url           string
		requireReport bool
	}
	endpoints := []endpoint{
		{"dashboard SPA", baseURL + basePath + "/", false},
		{"overview API", baseURL + "/api/v1/dashboard/overview", false},
		{"gate-checks API", baseURL + "/api/v1/dashboard/gate-checks", requireGateCheckReport},
		{"gate endpoint", baseURL + "/gate", false},
	}

	client := &http.Client{Timeout: 5 * time.Second}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ENDPOINT\tURL\tSTATUS")
	fmt.Fprintln(w, "--------\t---\t------")

	var failed []string
	for _, ep := range endpoints {
		resp, err := client.Get(ep.url) //nolint:noctx // intentional diagnostic request
		if err != nil {
			fmt.Fprintf(w, "%s\t%s\t✗ unreachable (%v)\n", ep.name, ep.url, err)
			failed = append(failed, ep.name)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			if readErr != nil {
				fmt.Fprintf(w, "%s\t%s\t✗ read error (%v)\n", ep.name, ep.url, readErr)
				failed = append(failed, ep.name)
				continue
			}
			if ep.requireReport {
				if err := validateGateChecksBody(body); err != nil {
					fmt.Fprintf(w, "%s\t%s\t✗ %s\n", ep.name, ep.url, err)
					failed = append(failed, ep.name)
					continue
				}
			}
			fmt.Fprintf(w, "%s\t%s\t✓ %d\n", ep.name, ep.url, resp.StatusCode)
		} else {
			fmt.Fprintf(w, "%s\t%s\t✗ %d\n", ep.name, ep.url, resp.StatusCode)
			failed = append(failed, ep.name)
		}
	}
	w.Flush()

	if len(failed) > 0 {
		return fmt.Errorf("dashboard check: %d endpoint(s) failed: %s",
			len(failed), strings.Join(failed, ", "))
	}
	fmt.Println("\nAll endpoints healthy.")
	return nil
}

func validateGateChecksBody(body []byte) error {
	var payload struct {
		Report json.RawMessage `json:"report"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("invalid gate-checks JSON (%w)", err)
	}
	report := bytes.TrimSpace(payload.Report)
	if len(report) == 0 || bytes.Equal(report, []byte("null")) {
		return fmt.Errorf("gate-check report missing")
	}
	return nil
}

func runDashboardFixture(bindHost string, bindPort int, basePath, repoPath, reportPath, fixtureDirPath string, checkMode, reportPathSet bool) error {
	basePath = normalizeDashboardBasePath(basePath)
	if repoPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		repoPath = wd
	}
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	repoPath = absRepoPath

	if reportPath == "" {
		reportPath = filepath.Join(repoPath, gatecheck.DefaultReportPath)
	} else {
		absReportPath, err := filepath.Abs(reportPath)
		if err != nil {
			return fmt.Errorf("resolve report path: %w", err)
		}
		reportPath = absReportPath
	}

	// When --fixture-dir is provided, load report.json from it if --report-path
	// was not explicitly set. Sessions and activity are always seeded from the dir.
	var resolvedFixtureDir string
	if fixtureDirPath != "" {
		resolvedPath := fixtureDirPath
		if !filepath.IsAbs(resolvedPath) {
			resolvedPath = filepath.Join(repoPath, fixtureDirPath)
		}
		abs, err := filepath.Abs(resolvedPath)
		if err != nil {
			return fmt.Errorf("resolve fixture-dir: %w", err)
		}
		resolvedFixtureDir = abs
		if !reportPathSet {
			candidate := filepath.Join(resolvedFixtureDir, "report.json")
			if _, statErr := os.Stat(candidate); statErr == nil {
				reportPath = candidate
				reportPathSet = true
			}
		}
	}

	requireGateCheckReport := reportPathSet
	if !requireGateCheckReport {
		_, statErr := os.Stat(reportPath)
		requireGateCheckReport = statErr == nil
	}

	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	allowPortRetry := bindPort == 0
	if bindPort == 0 {
		bindPort = configpkg.DefaultAPIServerPort
	}

	fixtureDir, err := os.MkdirTemp("", "codero-dashboard-fixture-*")
	if err != nil {
		return fmt.Errorf("create fixture dir: %w", err)
	}
	defer os.RemoveAll(fixtureDir)

	db, err := state.Open(filepath.Join(fixtureDir, "fixture.db"))
	if err != nil {
		return fmt.Errorf("open fixture db: %w", err)
	}
	defer db.Close()

	// Seed fixture data (sessions, activity) from --fixture-dir when provided.
	if resolvedFixtureDir != "" {
		if _, loadErr := dashboardpkg.LoadFixtureDir(context.Background(), db.Unwrap(), resolvedFixtureDir); loadErr != nil {
			return fmt.Errorf("load fixture dir %q: %w", resolvedFixtureDir, loadErr)
		}
	}

	restoreRepoPath := withEnv("CODERO_REPO_PATH", repoPath)
	defer restoreRepoPath()
	restoreReportPath := withEnv("CODERO_GATE_CHECK_REPORT_PATH", reportPath)
	defer restoreReportPath()

	redisClient := redislib.New("127.0.0.1:0", "")
	defer redisClient.Close()

	var (
		obs      *daemon.ObservabilityServer
		baseURL  string
		startErr error
	)
	maxAttempts := 1
	if allowPortRetry {
		maxAttempts = 10
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		port := bindPort + attempt
		obs = daemon.NewObservabilityServer(redisClient, nil, nil, db.Unwrap(), bindHost, strconv.Itoa(port), basePath, version)
		obs.Start()

		baseURL = dashboardBaseURL(bindHost, port)
		startErr = waitForDashboard(baseURL + "/gate")
		if startErr == nil {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = obs.Stop(ctx)
		cancel()
		if !allowPortRetry {
			return fmt.Errorf("start dashboard fixture: %w", startErr)
		}
	}
	if startErr != nil {
		return fmt.Errorf("start dashboard fixture after %d attempts: %w", maxAttempts, startErr)
	}

	if checkMode {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = obs.Stop(ctx)
		}()
		return runDashboardCheckWithOptions(baseURL, basePath, requireGateCheckReport)
	}

	fmt.Printf("\nFixture dashboard is serving locally.\n")
	fmt.Printf("Dashboard URL:  %s%s/\n", baseURL, basePath)
	fmt.Printf("Overview API:   %s/api/v1/dashboard/overview\n", baseURL)
	fmt.Printf("Gate Checks:    %s/api/v1/dashboard/gate-checks\n", baseURL)
	fmt.Printf("Gate endpoint:  %s/gate\n", baseURL)
	fmt.Fprintln(os.Stderr, "Press Ctrl+C to stop the fixture server.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return obs.Stop(ctx)
}

func waitForDashboard(url string) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	lastErr := fmt.Errorf("dashboard probe timed out: %s", url)
	for {
		resp, err := client.Get(url) //nolint:noctx // startup probe
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return nil
			}
			lastErr = fmt.Errorf("dashboard probe returned %d: %s", resp.StatusCode, url)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func normalizeDashboardBasePath(basePath string) string {
	if basePath == "" {
		return "/dashboard"
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = strings.TrimRight(basePath, "/")
	if basePath == "" {
		return "/dashboard"
	}
	return basePath
}

func dashboardBaseURL(bindHost string, bindPort int) string {
	host := bindHost
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(bindPort))
}

func apiBindHostPort(addr string) (string, int, error) {
	host, port, err := configpkg.ParseAPIServerAddr(addr)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func withEnv(key, value string) func() {
	prev, hadPrev := os.LookupEnv(key)
	_ = os.Setenv(key, value)
	return func() {
		if hadPrev {
			_ = os.Setenv(key, prev)
			return
		}
		_ = os.Unsetenv(key)
	}
}

// isInteractiveEnv returns true in an interactive local environment where
// opening a browser makes sense. It checks for a display server and TTY.
func isInteractiveEnv() bool {
	// In CI/headless environments DISPLAY (X11) or WAYLAND_DISPLAY is absent.
	// On macOS there is no DISPLAY but open(1) works, so we check TERM_PROGRAM.
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return false
	}
	switch runtime.GOOS {
	case "darwin":
		return true
	case "linux":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	default:
		return true
	}
}

// openBrowser opens url in the default system browser.
func openBrowser(url string) error {
	parsed, err := neturl.Parse(url)
	if err != nil {
		return fmt.Errorf("invalid dashboard URL %q: %w", url, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid dashboard URL %q: expected http(s) URL with host", url)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		fmt.Printf("Open %s in your browser.\n", url)
		return nil
	}
	fmt.Printf("Opening %s ...\n", url)
	return cmd.Start()
}

// portsCmd implements "codero ports": diagnostic output of all active/effective
// network bindings based on current configuration.
func portsCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ports",
		Short: "Show effective network bindings",
		Long: `Print all configured network addresses and URLs for running codero services.

Useful for diagnosing port conflicts and verifying proxy configuration.

Examples:
  codero ports
  codero ports --config codero.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cfgErr := loadConfig(*configPath)

			// Use config values or built-in defaults if config is unavailable.
			obsHost := ""
			obsPort := configpkg.DefaultAPIServerPort
			dashBasePath := "/dashboard"
			dashPublicURL := ""
			webhookEnabled := false
			webhookPort := 9090

			if cfg != nil {
				bindHost, bindPort, err := apiBindHostPort(cfg.APIServer.Addr)
				if err != nil {
					if cfgErr == nil {
						cfgErr = err
					}
				} else {
					obsHost, obsPort = bindHost, bindPort
				}
				if cfg.DashboardBasePath != "" {
					dashBasePath = cfg.DashboardBasePath
				}
				dashPublicURL = cfg.DashboardPublicBaseURL
				webhookEnabled = cfg.Webhook.Enabled
				webhookPort = cfg.Webhook.Port
			}

			displayHost := obsHost
			if displayHost == "" {
				displayHost = "0.0.0.0 (all interfaces)"
			}

			localBase := dashboardBaseURL(obsHost, obsPort)
			dashURL := localBase + dashBasePath + "/"
			if dashPublicURL != "" {
				dashURL = strings.TrimRight(dashPublicURL, "/") + dashBasePath + "/"
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SERVICE\tBIND\tURL")
			fmt.Fprintln(w, "-------\t----\t---")
			fmt.Fprintf(w, "observability\t%s:%d\t%s\n", displayHost, obsPort, localBase)
			fmt.Fprintf(w, "dashboard SPA\t%s:%d\t%s\n", displayHost, obsPort, dashURL)
			fmt.Fprintf(w, "gate endpoint\t%s:%d\t%s/gate\n", displayHost, obsPort, localBase)
			fmt.Fprintf(w, "overview API\t%s:%d\t%s/api/v1/dashboard/overview\n", displayHost, obsPort, localBase)
			if webhookEnabled {
				fmt.Fprintf(w, "webhook receiver\t0.0.0.0:%d\t(HTTP, path /webhook/github)\n", webhookPort)
			} else {
				fmt.Fprintf(w, "webhook receiver\t-\t(disabled; polling-only mode)\n")
			}
			w.Flush()

			// Conflict detection: warn if observability and webhook ports collide.
			if webhookEnabled && webhookPort == obsPort {
				fmt.Printf("\n⚠  WARNING: api_server.addr and webhook.port both use %d — port conflict!\n", obsPort)
				fmt.Println("   Fix: set different values for api_server.addr and webhook.port in config.")
			}

			if cfgErr != nil {
				fmt.Fprintf(os.Stderr, "\nnote: config load error (%v); showing defaults\n", cfgErr)
			}

			return nil
		},
	}
}
