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
		TelegramEnabled:   false,
		TelegramBotToken:  "",
		TelegramChatID:    "",
		TelegramAPIBase:   "https://api.telegram.org",
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
	if settings.TelegramAPIBase == "" {
		settings.TelegramAPIBase = def.TelegramAPIBase
	}
	sanitized, changed := s.sanitizeEnginePaths(settings)
	if changed {
		if err := s.Save(sanitized); err != nil {
			// still return sanitized in-memory values even if persist fails
			return sanitized, nil
		}
		return sanitized, nil
	}
	return settings, nil
}

func (s *Service) Get() (model.Settings, error) {
	return s.EnsureDefaults()
}

func (s *Service) Save(settings model.Settings) error {
	settings.UpdatedAt = time.Now()
	sanitized, _ := s.sanitizeEnginePaths(settings)
	settings = sanitized
	if err := os.MkdirAll(settings.CFSTWorkingDir, 0o755); err != nil {
		return err
	}
	if settings.CFSTResultFile != "" {
		_ = os.MkdirAll(filepath.Dir(settings.CFSTResultFile), 0o755)
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.store.SetMeta(settingsKey, string(b))
}

// sanitizeEnginePaths relocates engine paths into the current data dir when the
// imported/old config points at an unwritable location (common after moving
// from /root/... to /opt/cfst-panel while running as user cfst).
func (s *Service) sanitizeEnginePaths(settings model.Settings) (model.Settings, bool) {
	def := DefaultSettings(s.dataDir)
	changed := false

	set := func(dst *string, val string) {
		if strings.TrimSpace(*dst) != val {
			*dst = val
			changed = true
		}
	}

	// Working dir must be writable by the service user.
	wd := strings.TrimSpace(settings.CFSTWorkingDir)
	if wd == "" || !isWritableDir(wd) || shouldRelocatePath(wd, s.dataDir) {
		set(&settings.CFSTWorkingDir, s.dataDir)
	}

	// Binary: prefer existing bundled binary under data dir.
	bin := strings.TrimSpace(settings.CFSTBinary)
	switch {
	case bin == "":
		set(&settings.CFSTBinary, def.CFSTBinary)
	case fileExists(bin) && !shouldRelocatePath(bin, s.dataDir):
		// keep custom existing binary
	default:
		candidate := filepath.Join(s.dataDir, filepath.Base(bin))
		if fileExists(def.CFSTBinary) {
			set(&settings.CFSTBinary, def.CFSTBinary)
		} else if fileExists(candidate) {
			set(&settings.CFSTBinary, candidate)
		} else if shouldRelocatePath(bin, s.dataDir) || !fileExists(bin) {
			set(&settings.CFSTBinary, def.CFSTBinary)
		}
	}

	// Result file parent must be writable.
	res := strings.TrimSpace(settings.CFSTResultFile)
	if res == "" {
		set(&settings.CFSTResultFile, def.CFSTResultFile)
	} else if shouldRelocatePath(res, s.dataDir) || !isWritableDir(filepath.Dir(res)) {
		base := filepath.Base(res)
		if base == "." || base == string(filepath.Separator) || base == "" {
			base = "result.csv"
		}
		set(&settings.CFSTResultFile, filepath.Join(s.dataDir, base))
	}

	// IP file: if missing/inaccessible, fall back to data dir defaults when present.
	ipf := strings.TrimSpace(settings.CFSTIPFile)
	if ipf == "" {
		if fileExists(def.CFSTIPFile) {
			set(&settings.CFSTIPFile, def.CFSTIPFile)
		}
	} else if shouldRelocatePath(ipf, s.dataDir) || !fileExists(ipf) {
		base := filepath.Base(ipf)
		candidate := filepath.Join(s.dataDir, base)
		switch {
		case fileExists(candidate):
			set(&settings.CFSTIPFile, candidate)
		case fileExists(def.CFSTIPFile):
			set(&settings.CFSTIPFile, def.CFSTIPFile)
		case shouldRelocatePath(ipf, s.dataDir):
			set(&settings.CFSTIPFile, candidate)
		}
	}

	return settings, changed
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func isWritableDir(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(path, ".cfst-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func shouldRelocatePath(path, dataDir string) bool {
	path = strings.TrimSpace(path)
	dataDir = strings.TrimSpace(dataDir)
	if path == "" || dataDir == "" {
		return false
	}
	absPath, err1 := filepath.Abs(path)
	absData, err2 := filepath.Abs(dataDir)
	if err1 != nil || err2 != nil {
		return false
	}
	absPath = filepath.Clean(absPath)
	absData = filepath.Clean(absData)
	sep := string(filepath.Separator)
	if absPath == absData || strings.HasPrefix(absPath, absData+sep) {
		return false
	}
	// Common migration case: old manual run under /root/...
	lower := strings.ToLower(strings.ReplaceAll(absPath, "\\", "/"))
	if strings.HasPrefix(lower, "/root/") || strings.Contains(lower, "/root/cloudflarest") {
		return true
	}
	// If parent dir is not writable, relocate.
	parent := absPath
	if st, err := os.Stat(absPath); err == nil && !st.IsDir() {
		parent = filepath.Dir(absPath)
	}
	return !isWritableDir(parent)
}
