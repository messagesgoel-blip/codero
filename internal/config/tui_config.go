package config

import (
	"os"
	"strconv"
	"strings"
)

// TUIConfig holds all 38 CODERO_TUI_* configuration variables from
// Real-Time Views v1 §5. Every field has a spec-mandated default.
type TUIConfig struct {
	Enabled              bool   `yaml:"enabled"`
	DefaultView          string `yaml:"default_view"`
	Theme                string `yaml:"theme"`
	AltScreen            bool   `yaml:"alt_screen"`
	Mouse                bool   `yaml:"mouse"`
	PollInterval         int    `yaml:"poll_interval"`
	SSEEnabled           bool   `yaml:"sse_enabled"`
	SSEReconnectMax      int    `yaml:"sse_reconnect_max"`
	SessionTableSort     string `yaml:"session_table_sort"`
	SessionTableMaxRows  int    `yaml:"session_table_max_rows"`
	TimelineShowDuration bool   `yaml:"timeline_show_duration"`
	TimelineShowTS       bool   `yaml:"timeline_show_timestamps"`
	PipelineAnimation    bool   `yaml:"pipeline_animation"`
	GateAutoRefresh      bool   `yaml:"gate_auto_refresh"`
	GateRefreshInterval  int    `yaml:"gate_refresh_interval"`
	EventsMaxLines       int    `yaml:"events_max_lines"`
	EventsFilterDefault  string `yaml:"events_filter_default"`
	ArchivesPageSize     int    `yaml:"archives_page_size"`
	HeartbeatWarnSec     int    `yaml:"heartbeat_warn_seconds"`
	HeartbeatAlertSec    int    `yaml:"heartbeat_alert_seconds"`
	OverviewRecentEvents int    `yaml:"overview_recent_events"`
	OverviewSystemHealth bool   `yaml:"overview_system_health"`
	KeyOverview          string `yaml:"keybind_overview"`
	KeySession           string `yaml:"keybind_session"`
	KeyGate              string `yaml:"keybind_gate"`
	KeyQueue             string `yaml:"keybind_queue"`
	KeyPipeline          string `yaml:"keybind_pipeline"`
	KeyEvents            string `yaml:"keybind_events"`
	KeyBranch            string `yaml:"keybind_branch"`
	KeyArchives          string `yaml:"keybind_archives"`
	KeyCompliance        string `yaml:"keybind_compliance"`
	KeySettings          string `yaml:"keybind_settings"`
	KeyHelp              string `yaml:"keybind_help"`
	KeyQuit              string `yaml:"keybind_quit"`
	KeyRefresh           string `yaml:"keybind_refresh"`
	BorderStyle          string `yaml:"border_style"`
	StatusBar            bool   `yaml:"status_bar"`
	BellOnMerge          bool   `yaml:"bell_on_merge"`
	BellOnGateFail       bool   `yaml:"bell_on_gate_fail"`
	BellOnSessionLost    bool   `yaml:"bell_on_session_lost"`
}

// DefaultTUIConfig returns spec-mandated defaults (§5).
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Enabled:              true,
		DefaultView:          "overview",
		Theme:                "dracula",
		AltScreen:            true,
		Mouse:                true,
		PollInterval:         1,
		SSEEnabled:           false,
		SSEReconnectMax:      30,
		SessionTableSort:     "checkpoint",
		SessionTableMaxRows:  50,
		TimelineShowDuration: true,
		TimelineShowTS:       true,
		PipelineAnimation:    true,
		GateAutoRefresh:      true,
		GateRefreshInterval:  1,
		EventsMaxLines:       200,
		EventsFilterDefault:  "all",
		ArchivesPageSize:     25,
		HeartbeatWarnSec:     30,
		HeartbeatAlertSec:    60,
		OverviewRecentEvents: 5,
		OverviewSystemHealth: true,
		KeyOverview:          "o",
		KeySession:           "s",
		KeyGate:              "g",
		KeyQueue:             "q",
		KeyPipeline:          "p",
		KeyEvents:            "e",
		KeyBranch:            "b",
		KeyArchives:          "a",
		KeyCompliance:        "r",
		KeySettings:          "/",
		KeyHelp:              "?",
		KeyQuit:              "ctrl+c",
		KeyRefresh:           "ctrl+r",
		BorderStyle:          "rounded",
		StatusBar:            true,
		BellOnMerge:          true,
		BellOnGateFail:       true,
		BellOnSessionLost:    true,
	}
}

// applyTUIEnvOverrides reads CODERO_TUI_* environment variables.
func applyTUIEnvOverrides(c *TUIConfig) {
	if v := os.Getenv("CODERO_TUI_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Enabled = b
		}
	}
	if v := os.Getenv("CODERO_TUI_DEFAULT_VIEW"); v != "" {
		c.DefaultView = v
	}
	if v := os.Getenv("CODERO_TUI_THEME"); v != "" {
		c.Theme = strings.ToLower(v)
	}
	if v := os.Getenv("CODERO_TUI_ALT_SCREEN"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.AltScreen = b
		}
	}
	if v := os.Getenv("CODERO_TUI_MOUSE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Mouse = b
		}
	}
	if v := os.Getenv("CODERO_TUI_POLL_INTERVAL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.PollInterval = i
		}
	}
	if v := os.Getenv("CODERO_TUI_SSE_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.SSEEnabled = b
		}
	}
	if v := os.Getenv("CODERO_TUI_SSE_RECONNECT_MAX"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.SSEReconnectMax = i
		}
	}
	if v := os.Getenv("CODERO_TUI_SESSION_TABLE_SORT"); v != "" {
		c.SessionTableSort = v
	}
	if v := os.Getenv("CODERO_TUI_SESSION_TABLE_MAX_ROWS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.SessionTableMaxRows = i
		}
	}
	if v := os.Getenv("CODERO_TUI_TIMELINE_SHOW_DURATION"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.TimelineShowDuration = b
		}
	}
	if v := os.Getenv("CODERO_TUI_TIMELINE_SHOW_TIMESTAMPS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.TimelineShowTS = b
		}
	}
	if v := os.Getenv("CODERO_TUI_PIPELINE_ANIMATION"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.PipelineAnimation = b
		}
	}
	if v := os.Getenv("CODERO_TUI_GATE_AUTO_REFRESH"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.GateAutoRefresh = b
		}
	}
	if v := os.Getenv("CODERO_TUI_GATE_REFRESH_INTERVAL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.GateRefreshInterval = i
		}
	}
	if v := os.Getenv("CODERO_TUI_EVENTS_MAX_LINES"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.EventsMaxLines = i
		}
	}
	if v := os.Getenv("CODERO_TUI_EVENTS_FILTER_DEFAULT"); v != "" {
		c.EventsFilterDefault = v
	}
	if v := os.Getenv("CODERO_TUI_ARCHIVES_PAGE_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.ArchivesPageSize = i
		}
	}
	if v := os.Getenv("CODERO_TUI_HEARTBEAT_WARN_SECONDS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.HeartbeatWarnSec = i
		}
	}
	if v := os.Getenv("CODERO_TUI_HEARTBEAT_ALERT_SECONDS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.HeartbeatAlertSec = i
		}
	}
	if v := os.Getenv("CODERO_TUI_OVERVIEW_RECENT_EVENTS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.OverviewRecentEvents = i
		}
	}
	if v := os.Getenv("CODERO_TUI_OVERVIEW_SYSTEM_HEALTH"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.OverviewSystemHealth = b
		}
	}
	// Keybind overrides.
	kbEnvs := map[string]*string{
		"CODERO_TUI_KEYBIND_OVERVIEW":   &c.KeyOverview,
		"CODERO_TUI_KEYBIND_SESSION":    &c.KeySession,
		"CODERO_TUI_KEYBIND_GATE":       &c.KeyGate,
		"CODERO_TUI_KEYBIND_QUEUE":      &c.KeyQueue,
		"CODERO_TUI_KEYBIND_PIPELINE":   &c.KeyPipeline,
		"CODERO_TUI_KEYBIND_EVENTS":     &c.KeyEvents,
		"CODERO_TUI_KEYBIND_BRANCH":     &c.KeyBranch,
		"CODERO_TUI_KEYBIND_ARCHIVES":   &c.KeyArchives,
		"CODERO_TUI_KEYBIND_COMPLIANCE": &c.KeyCompliance,
		"CODERO_TUI_KEYBIND_SETTINGS":   &c.KeySettings,
		"CODERO_TUI_KEYBIND_HELP":       &c.KeyHelp,
		"CODERO_TUI_KEYBIND_QUIT":       &c.KeyQuit,
		"CODERO_TUI_KEYBIND_REFRESH":    &c.KeyRefresh,
	}
	for envKey, target := range kbEnvs {
		if v := os.Getenv(envKey); v != "" {
			*target = v
		}
	}
	if v := os.Getenv("CODERO_TUI_BORDER_STYLE"); v != "" {
		c.BorderStyle = v
	}
	if v := os.Getenv("CODERO_TUI_STATUS_BAR"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.StatusBar = b
		}
	}
	if v := os.Getenv("CODERO_TUI_BELL_ON_MERGE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.BellOnMerge = b
		}
	}
	if v := os.Getenv("CODERO_TUI_BELL_ON_GATE_FAIL"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.BellOnGateFail = b
		}
	}
	if v := os.Getenv("CODERO_TUI_BELL_ON_SESSION_LOST"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.BellOnSessionLost = b
		}
	}
}
