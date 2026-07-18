package config

import (
	"encoding/json"
	"os"
	"strings"
	"path/filepath"
	"time"

	"github.com/debbide/cfst-panel/internal/model"
	"github.com/debbide/cfst-panel/internal/store"
)

const settingsKey = "app_settings"

type Service struct {
	store   *store.Store
	dataDir string
}

func NewService(st *store.Store, dataDir string) *Service {
	return &Service{store: st, dataDir: dataDir}
}

func (s *Service) DataDir() string { return s.dataDir }

func DefaultSettings(dataDir string) model.Settings {
	return model.Settings{
		ListenAddr:        "0.0.0.0:8787",
		PanelUser:         "admin",
		PanelPass:         "admin123",
		SessionHours:      72,
		Timezone:          "Asia/Shanghai",
		CFAPIToken:        "",
		CFZoneID:          "",
		CFZoneName:        "",
		CFProxied:         false,
		CFTTL:             1,
		CFRecordType:      "A",
		CFSTBinary:        filepath.Join(dataDir, "CloudflareST"),
		CFSTIPFile:        filepath.Join(dataDir, "ip.txt"),
		CFSTIPListURL:     "https://cf.090227.xyz/ct?ips=20",
		CFSTIPVersion:     "4",
		CFSTExtraArgs:     "",
		CFSTTimeoutSec:    600,
		CFSTResultFile:    filepath.Join(dataDir, "result.csv"),
		CFSTWorkingDir:    dataDir,
		CFSTDownloadURL:   "",
		CFSTThreads:       80,
		CFSTDelay:         4,
		CFSTSampleCount:   0,
		CFSTSpeedTestURL:  "",
		CFSTTestCount:     10,
		CFSTPort:          443,
		CFSTEnableHttping: false,
		MaxLatencyMS:      300,
		MinSpeedMbps:      1,
		MaxLossPercent:    10,
		TopN:              10,
		ResolveIPCount:    6,
		PreferLowerLoss:   true,
		UpdateOnlyBetter:  false,
		ForceUpdate:       false,
		DryRun:            false,
		ScheduleEnabled:   false,
		CronExpr:          "0 */2 * * *",
		WebhookEnabled:    false,
		WebhookURL:        "",
		WebhookSecret:     "",
		UpdatedAt:         time.Now(),
	}
}

func (s *Service) EnsureDefaults() (model.Settings, error) {
	raw, err := s.store.GetMeta(settingsKey)
	if err != nil {
		return model.Settings{}, err
	}
	if raw == "" {
		def := DefaultSettings(s.dataDir)
		if err := s.Save(def); err != nil {
			return model.Settings{}, err
		}
		return def, nil
	}
	var settings model.Settings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return model.Settings{}, err
	}
	def := DefaultSettings(s.dataDir)
	if !strings.Contains(raw, `"cfst_sample_count"`) {
		settings.CFSTSampleCount = def.CFSTSampleCount
	}
	if !strings.Contains(raw, `"cfst_ip_list_url"`) {
		settings.CFSTIPListURL = def.CFSTIPListURL
	}
	if settings.ListenAddr == "" {
		settings.ListenAddr = def.ListenAddr
	}
	if settings.PanelUser == "" {
		settings.PanelUser = def.PanelUser
	}
	if settings.PanelPass == "" {
		settings.PanelPass = def.PanelPass
	}
	if settings.SessionHours <= 0 {
		settings.SessionHours = def.SessionHours
	}
	if settings.Timezone == "" {
		settings.Timezone = def.Timezone
	}
	if settings.CFRecordType == "" {
		settings.CFRecordType = def.CFRecordType
	}
	if settings.CFSTIPVersion == "" {
		settings.CFSTIPVersion = def.CFSTIPVersion
	}
	if settings.CFSTIPVersion != "4" && settings.CFSTIPVersion != "6" && settings.CFSTIPVersion != "both" {
		settings.CFSTIPVersion = "4"
	}
	if settings.CFSTBinary == "" {
		settings.CFSTBinary = def.CFSTBinary
	}
	if settings.CFSTResultFile == "" {
		settings.CFSTResultFile = def.CFSTResultFile
	}
	if settings.CFSTWorkingDir == "" {
		settings.CFSTWorkingDir = def.CFSTWorkingDir
	}
	if settings.CFSTTimeoutSec <= 0 {
		settings.CFSTTimeoutSec = def.CFSTTimeoutSec
	}
	if settings.CFSTThreads <= 0 {
		settings.CFSTThreads = def.CFSTThreads
	}
	if settings.CFSTDelay <= 0 {
		settings.CFSTDelay = def.CFSTDelay
	}
	// Older panel builds mistakenly used 300 for -t (latency test count).
	// CloudflareST's -t is "times per IP", official default is 4. 300 makes scans crawl.
	if settings.CFSTDelay > 20 {
		settings.CFSTDelay = def.CFSTDelay
	}
	if settings.CFSTThreads > 1000 {
		settings.CFSTThreads = def.CFSTThreads
	}
	if settings.CFSTTestCount <= 0 {
		settings.CFSTTestCount = def.CFSTTestCount
	}
	if settings.CFSTPort <= 0 {
		settings.CFSTPort = def.CFSTPort
	}
	if settings.CFSTSampleCount < 0 {
		settings.CFSTSampleCount = def.CFSTSampleCount
	}
	if settings.TopN <= 0 {
		settings.TopN = def.TopN
	}
	if !strings.Contains(raw, `"resolve_ip_count"`) || settings.ResolveIPCount <= 0 {
		settings.ResolveIPCount = def.ResolveIPCount
	}
	if settings.ResolveIPCount > 20 {
		settings.ResolveIPCount = 20
	}
	if settings.CronExpr == "" {
		settings.CronExpr = def.CronExpr
	}
	return settings, nil
}

func (s *Service) Get() (model.Settings, error) {
	return s.EnsureDefaults()
}

func (s *Service) Save(settings model.Settings) error {
	settings.UpdatedAt = time.Now()
	if settings.CFSTResultFile == "" {
		settings.CFSTResultFile = filepath.Join(s.dataDir, "result.csv")
	}
	if settings.CFSTWorkingDir == "" {
		settings.CFSTWorkingDir = s.dataDir
	}
	if err := os.MkdirAll(settings.CFSTWorkingDir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.store.SetMeta(settingsKey, string(b))
}
