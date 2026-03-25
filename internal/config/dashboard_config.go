package config

import (
	"os"
	"strconv"
)

// DashboardConfig holds all 29 CODERO_DASHBOARD_* configuration variables
// from Real-Time Views v1 §6. Fields that were already on the top-level Config
// (DashboardBasePath, DashboardPublicBaseURL) are excluded here; they remain
// on Config for backward compatibility.
type DashboardConfig struct {
	Enabled              bool   `yaml:"enabled"`
	CORSOrigins          string `yaml:"cors_origins"`
	AuthEnabled          bool   `yaml:"auth_enabled"`
	AuthTokenPath        string `yaml:"auth_token_path"`
	SSEEnabled           bool   `yaml:"sse_enabled"`
	SSEHeartbeat         int    `yaml:"sse_heartbeat"`
	SSEBufferSize        int    `yaml:"sse_buffer_size"`
	Theme                string `yaml:"theme"`
	RefreshInterval      int    `yaml:"refresh_interval"`
	SessionTablePageSize int    `yaml:"session_table_page_size"`
	ArchivesPageSize     int    `yaml:"archives_page_size"`
	EventsPageSize       int    `yaml:"events_page_size"`
	TimelineShowAll      bool   `yaml:"timeline_show_all"`
	PipelineBoard        bool   `yaml:"pipeline_board"`
	GateLivePoll         int    `yaml:"gate_live_poll"`
	ChatEnabled          bool   `yaml:"chat_enabled"`
	MergeApproveEnabled  bool   `yaml:"merge_approve_enabled"`
	MergeRejectEnabled   bool   `yaml:"merge_reject_enabled"`
	MergeForceEnabled    bool   `yaml:"merge_force_enabled"`
	SettingsWriteEnabled bool   `yaml:"settings_write_enabled"`
	ComplianceView       bool   `yaml:"compliance_view"`
	QueueView            bool   `yaml:"queue_view"`
	ArchiveView          bool   `yaml:"archive_view"`
	StaticDir            string `yaml:"static_dir"`
	MaxOpenConnections   int    `yaml:"max_open_connections"`
	NotificationSound    bool   `yaml:"notification_sound"`
	AutoScrollEvents     bool   `yaml:"auto_scroll_events"`
	CompactMode          bool   `yaml:"compact_mode"`
}

// DefaultDashboardConfig returns spec-mandated defaults (§6).
func DefaultDashboardConfig() DashboardConfig {
	return DashboardConfig{
		Enabled:              true,
		CORSOrigins:          "*",
		AuthEnabled:          false,
		AuthTokenPath:        "$HOME/.codero/dashboard-token",
		SSEEnabled:           true,
		SSEHeartbeat:         15,
		SSEBufferSize:        100,
		Theme:                "dark",
		RefreshInterval:      5,
		SessionTablePageSize: 50,
		ArchivesPageSize:     50,
		EventsPageSize:       100,
		TimelineShowAll:      true,
		PipelineBoard:        true,
		GateLivePoll:         1,
		ChatEnabled:          true,
		MergeApproveEnabled:  true,
		MergeRejectEnabled:   true,
		MergeForceEnabled:    false,
		SettingsWriteEnabled: true,
		ComplianceView:       true,
		QueueView:            true,
		ArchiveView:          true,
		StaticDir:            "",
		MaxOpenConnections:   100,
		NotificationSound:    true,
		AutoScrollEvents:     true,
		CompactMode:          false,
	}
}

// applyDashboardEnvOverrides reads CODERO_DASHBOARD_* environment variables.
// Note: CODERO_DASHBOARD_BASE_PATH and CODERO_DASHBOARD_PUBLIC_BASE_URL are
// handled in config.go's applyEnvOverrides for backward compatibility.
func applyDashboardEnvOverrides(c *DashboardConfig) {
	if v := os.Getenv("CODERO_DASHBOARD_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Enabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_CORS_ORIGINS"); v != "" {
		c.CORSOrigins = v
	}
	if v := os.Getenv("CODERO_DASHBOARD_AUTH_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.AuthEnabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_AUTH_TOKEN_PATH"); v != "" {
		c.AuthTokenPath = v
	}
	if v := os.Getenv("CODERO_DASHBOARD_SSE_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.SSEEnabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_SSE_HEARTBEAT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.SSEHeartbeat = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_SSE_BUFFER_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.SSEBufferSize = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_THEME"); v != "" {
		c.Theme = v
	}
	if v := os.Getenv("CODERO_DASHBOARD_REFRESH_INTERVAL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.RefreshInterval = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_SESSION_TABLE_PAGE_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.SessionTablePageSize = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_ARCHIVES_PAGE_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.ArchivesPageSize = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_EVENTS_PAGE_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.EventsPageSize = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_TIMELINE_SHOW_ALL"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.TimelineShowAll = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_PIPELINE_BOARD"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.PipelineBoard = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_GATE_LIVE_POLL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.GateLivePoll = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_CHAT_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.ChatEnabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_MERGE_APPROVE_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.MergeApproveEnabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_MERGE_REJECT_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.MergeRejectEnabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_MERGE_FORCE_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.MergeForceEnabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_SETTINGS_WRITE_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.SettingsWriteEnabled = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_COMPLIANCE_VIEW"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.ComplianceView = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_QUEUE_VIEW"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.QueueView = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_ARCHIVE_VIEW"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.ArchiveView = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_STATIC_DIR"); v != "" {
		c.StaticDir = v
	}
	if v := os.Getenv("CODERO_DASHBOARD_MAX_OPEN_CONNECTIONS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.MaxOpenConnections = i
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_NOTIFICATION_SOUND"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.NotificationSound = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_AUTO_SCROLL_EVENTS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.AutoScrollEvents = b
		}
	}
	if v := os.Getenv("CODERO_DASHBOARD_COMPACT_MODE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.CompactMode = b
		}
	}
}
