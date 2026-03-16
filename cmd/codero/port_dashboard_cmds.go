package main

// port_dashboard_cmds.go — codero dashboard and codero ports commands.
//
// These commands give operators quick access to the running dashboard URL,
// health validation, and network binding diagnostics without having to
// inspect config files or query running processes directly.

import (
	"fmt"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// dashboardCmd implements "codero dashboard": prints the effective dashboard
// URL, optionally opens a browser, and can validate endpoint reachability.
func dashboardCmd(configPath *string) *cobra.Command {
	var (
		host      string
		port      int
		openBrws  bool
		checkMode bool
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Show dashboard URL and status",
		Long: `Print the effective dashboard URL and optionally validate reachability.

Examples:
  codero dashboard                   # print effective URL and endpoint list
  codero dashboard --check           # validate /dashboard/, /api/v1/dashboard/overview, /gate
  codero dashboard --open            # open dashboard in default browser (interactive only)
  codero dashboard --port 9090       # override port (useful when testing non-default setups)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, cfgErr := loadConfig(*configPath) // best-effort; fall back to defaults on error

			effectiveHost := host
			effectivePort := port
			basePath := "/dashboard"

			if cfg != nil {
				if effectiveHost == "" {
					effectiveHost = cfg.ObservabilityHost
				}
				if effectivePort == 0 {
					effectivePort = cfg.ObservabilityPort
				}
				if cfg.DashboardBasePath != "" {
					basePath = cfg.DashboardBasePath
				}
			}
			if effectiveHost == "" {
				effectiveHost = "localhost"
			}
			if effectivePort == 0 {
				effectivePort = 8080
			}
			if cfgErr != nil {
				fmt.Fprintf(os.Stderr, "note: config load error (%v); using defaults where needed\n", cfgErr)
			}

			// Determine base URL: prefer explicit public URL from config.
			baseURL := fmt.Sprintf("http://%s:%d", effectiveHost, effectivePort)
			if cfg != nil && cfg.DashboardPublicBaseURL != "" {
				baseURL = strings.TrimRight(cfg.DashboardPublicBaseURL, "/")
			}

			dashURL := baseURL + basePath + "/"
			overviewURL := baseURL + basePath + "/api/v1/dashboard/overview"

			fmt.Printf("Dashboard URL:  %s\n", dashURL)
			fmt.Printf("Overview API:   %s\n", overviewURL)
			fmt.Printf("Gate endpoint:  %s/gate\n", baseURL)

			if checkMode {
				return runDashboardCheck(baseURL, basePath)
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
	cmd.Flags().IntVar(&port, "port", 0, "daemon port (default: from config or 8080)")
	cmd.Flags().BoolVar(&openBrws, "open", false, "open dashboard in default browser (interactive only)")
	cmd.Flags().BoolVar(&checkMode, "check", false, "validate dashboard and API endpoints; exit non-zero on failure")

	return cmd
}

// runDashboardCheck GETs three critical endpoints and reports pass/fail.
// Returns a non-nil error if any endpoint is unreachable or returns non-2xx.
func runDashboardCheck(baseURL, basePath string) error {
	type endpoint struct {
		name string
		url  string
	}
	endpoints := []endpoint{
		{"dashboard SPA", baseURL + basePath + "/"},
		{"overview API", baseURL + basePath + "/api/v1/dashboard/overview"},
		{"gate endpoint", baseURL + "/gate"},
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
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
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
			obsPort := 8080
			dashBasePath := "/dashboard"
			dashPublicURL := ""
			webhookEnabled := false
			webhookPort := 9090

			if cfg != nil {
				obsHost = cfg.ObservabilityHost
				obsPort = cfg.ObservabilityPort
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

			localBase := fmt.Sprintf("http://localhost:%d", obsPort)
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
			fmt.Fprintf(w, "overview API\t%s:%d\t%s%s/api/v1/dashboard/overview\n", displayHost, obsPort, localBase, dashBasePath)
			if webhookEnabled {
				fmt.Fprintf(w, "webhook receiver\t0.0.0.0:%d\t(HTTP, path /webhook/github)\n", webhookPort)
			} else {
				fmt.Fprintf(w, "webhook receiver\t-\t(disabled; polling-only mode)\n")
			}
			w.Flush()

			// Conflict detection: warn if observability and webhook ports collide.
			if webhookEnabled && webhookPort == obsPort {
				fmt.Printf("\n⚠  WARNING: observability_port and webhook.port both use %d — port conflict!\n", obsPort)
				fmt.Println("   Fix: set different values for observability_port and webhook.port in config.")
			}

			if cfgErr != nil {
				fmt.Fprintf(os.Stderr, "\nnote: config load error (%v); showing defaults\n", cfgErr)
			}

			return nil
		},
	}
}
