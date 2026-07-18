package model

import "time"

type Settings struct {
	// System
	ListenAddr   string `json:"listen_addr"`
	PanelUser    string `json:"panel_user"`
	PanelPass    string `json:"panel_pass"`
	SessionHours int    `json:"session_hours"`
	Timezone     string `json:"timezone"`

	// Cloudflare
	CFAPIToken   string `json:"cf_api_token"`
	CFZoneID     string `json:"cf_zone_id"`
	CFZoneName   string `json:"cf_zone_name"`
	CFProxied    bool   `json:"cf_proxied"`
	CFTTL        int    `json:"cf_ttl"`
	CFRecordType string `json:"cf_record_type"`

	// Engine
	CFSTBinary        string `json:"cfst_binary"`
	CFSTIPFile        string `json:"cfst_ip_file"`
	CFSTIPListURL     string `json:"cfst_ip_list_url"`
	CFSTIPVersion     string `json:"cfst_ip_version"`
	CFSTExtraArgs     string `json:"cfst_extra_args"`
	CFSTTimeoutSec    int    `json:"cfst_timeout_sec"`
	CFSTResultFile    string `json:"cfst_result_file"`
	CFSTWorkingDir    string `json:"cfst_working_dir"`
	CFSTDownloadURL   string `json:"cfst_download_url"`
	CFSTThreads       int    `json:"cfst_threads"`
	CFSTDelay         int    `json:"cfst_delay"`
	CFSTSpeedTestURL  string `json:"cfst_speed_test_url"`
	CFSTTestCount     int    `json:"cfst_test_count"`
	CFSTPort          int    `json:"cfst_port"`
	CFSTEnableHttping bool   `json:"cfst_enable_httping"`
	// CFSTSampleCount limits how many IPs are randomly sampled from CIDR lists.
	// 0 means use the IP file as-is (can be 1M+ addresses and take a very long time).
	CFSTSampleCount int `json:"cfst_sample_count"`

	// Strategy
	MaxLatencyMS     int     `json:"max_latency_ms"`
	MinSpeedMbps     float64 `json:"min_speed_mbps"`
	MaxLossPercent   float64 `json:"max_loss_percent"`
	TopN             int     `json:"top_n"`
	// ResolveIPCount is how many best IPs are written to each DNS name.
	ResolveIPCount int `json:"resolve_ip_count"`
	PreferLowerLoss  bool    `json:"prefer_lower_loss"`
	UpdateOnlyBetter bool    `json:"update_only_better"`
	ForceUpdate      bool    `json:"force_update"`
	DryRun           bool    `json:"dry_run"`

	// Schedule
	ScheduleEnabled bool   `json:"schedule_enabled"`
	CronExpr        string `json:"cron_expr"`

	// Notify
	// Telegram native notify. API base can be a Workers reverse proxy of api.telegram.org.
	TelegramEnabled  bool   `json:"telegram_enabled"`
	TelegramBotToken string `json:"telegram_bot_token"`
	TelegramChatID   string `json:"telegram_chat_id"`
	// TelegramAPIBase examples:
	//   https://api.telegram.org
	//   https://tg-api.xxx.workers.dev
	TelegramAPIBase string `json:"telegram_api_base"`

	// Legacy webhook fields (kept for old panel.json compatibility).
	WebhookEnabled bool   `json:"webhook_enabled,omitempty"`
	WebhookURL     string `json:"webhook_url,omitempty"`
	WebhookSecret  string `json:"webhook_secret,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
}

type DNSRecord struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Content    string    `json:"content"`
	TTL        int       `json:"ttl"`
	Proxied    bool      `json:"proxied"`
	Enabled    bool      `json:"enabled"`
	CFRecordID string    `json:"cf_record_id"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskSuccess   TaskStatus = "success"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

type TaskTrigger string

const (
	TriggerManual   TaskTrigger = "manual"
	TriggerSchedule TaskTrigger = "schedule"
	TriggerAPI      TaskTrigger = "api"
)

type Task struct {
	ID           string      `json:"id"`
	Status       TaskStatus  `json:"status"`
	Trigger      TaskTrigger `json:"trigger"`
	Message      string      `json:"message"`
	SelectedIP   string      `json:"selected_ip"`
	SelectedLat  float64     `json:"selected_latency"`
	SelectedSpd  float64     `json:"selected_speed"`
	SelectedLoss float64     `json:"selected_loss"`
	UpdatedCount int         `json:"updated_count"`
	ResultJSON   string      `json:"result_json,omitempty"`
	LogText      string      `json:"log_text,omitempty"`
	StartedAt    *time.Time  `json:"started_at,omitempty"`
	FinishedAt   *time.Time  `json:"finished_at,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

type SpeedResult struct {
	IP      string  `json:"ip"`
	Latency float64 `json:"latency"`
	Speed   float64 `json:"speed"`
	Loss    float64 `json:"loss"`
	Colo    string  `json:"colo,omitempty"`
	Raw     string  `json:"raw,omitempty"`
}

type LogLevel string

const (
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

type LogEntry struct {
	ID        int64     `json:"id"`
	Level     LogLevel  `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Status struct {
	Version         string     `json:"version"`
	UptimeSec       int64      `json:"uptime_sec"`
	ListenAddr      string     `json:"listen_addr"`
	DataDir         string     `json:"data_dir"`
	CFSTBinaryOK    bool       `json:"cfst_binary_ok"`
	CFSTBinaryPath  string     `json:"cfst_binary_path"`
	CFSTBundled     bool       `json:"cfst_bundled"`
	Platform        string     `json:"platform"`
	ScheduleEnabled bool       `json:"schedule_enabled"`
	CronExpr        string     `json:"cron_expr"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	RunningTaskID   string     `json:"running_task_id,omitempty"`
	LastTask        *Task      `json:"last_task,omitempty"`
	RecordCount     int        `json:"record_count"`
	Timezone        string     `json:"timezone"`
}
