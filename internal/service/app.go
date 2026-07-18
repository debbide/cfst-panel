package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/debbide/cfst-panel/internal/cfst"
	"github.com/debbide/cfst-panel/internal/cfstbin"
	"github.com/debbide/cfst-panel/internal/cloudflare"
	"github.com/debbide/cfst-panel/internal/config"
	"github.com/debbide/cfst-panel/internal/model"
	"github.com/debbide/cfst-panel/internal/store"
)

const Version = "0.3.3"

type Scheduler interface {
	Reload(settings model.Settings) error
	Stop()
	NextRun() *time.Time
}

type App struct {
	store      *store.Store
	cfg        *config.Service
	dataDir    string
	listenAddr string
	runner     *cfst.Runner
	startedAt  time.Time
	sched      Scheduler

	mu         sync.Mutex
	runningID  string
	cancelFunc context.CancelFunc
}

func New(st *store.Store, cfg *config.Service, dataDir string) *App {
	return &App{
		store:     st,
		cfg:       cfg,
		dataDir:   dataDir,
		runner:    cfst.NewRunner(),
		startedAt: time.Now(),
	}
}

func (a *App) SetScheduler(s Scheduler) {
	a.sched = s
}

func (a *App) DataDir() string { return a.dataDir }

func (a *App) SetListenAddr(addr string) { a.listenAddr = addr }

func (a *App) ListenAddr() string {
	if a.listenAddr != "" {
		return a.listenAddr
	}
	return ""
}

func (a *App) Log(level model.LogLevel, source, message string) {
	_ = a.store.AddLog(level, source, message)
}

func (a *App) Login(username, password string) (string, time.Time, error) {
	settings, err := a.cfg.Get()
	if err != nil {
		return "", time.Time{}, err
	}
	if username != settings.PanelUser || password != settings.PanelPass {
		return "", time.Time{}, fmt.Errorf("invalid username or password")
	}
	return a.store.CreateSession(username, settings.SessionHours)
}

func (a *App) Logout(token string) error {
	return a.store.DeleteSession(token)
}

func (a *App) Auth(token string) (string, error) {
	user, _, err := a.store.GetSession(token)
	return user, err
}

func (a *App) GetSettings() (model.Settings, error) {
	return a.cfg.Get()
}

func (a *App) SaveSettings(settings model.Settings) (model.Settings, error) {
	if strings.TrimSpace(settings.PanelUser) == "" {
		return model.Settings{}, fmt.Errorf("panel user is required")
	}
	if strings.TrimSpace(settings.PanelPass) == "" {
		return model.Settings{}, fmt.Errorf("panel password is required")
	}
	if settings.SessionHours <= 0 {
		settings.SessionHours = 72
	}
	if settings.CronExpr == "" {
		settings.CronExpr = "0 */2 * * *"
	}
	if settings.ResolveIPCount <= 0 {
		settings.ResolveIPCount = 6
	}
	if settings.ResolveIPCount > 20 {
		settings.ResolveIPCount = 20
	}
	if err := a.cfg.Save(settings); err != nil {
		return model.Settings{}, err
	}
	if a.sched != nil {
		if err := a.sched.Reload(settings); err != nil {
			a.Log(model.LogWarn, "settings", "settings saved but scheduler reload failed: "+err.Error())
		}
	}
	a.Log(model.LogInfo, "settings", "settings updated")
	return a.cfg.Get()
}

func (a *App) ListRecords() ([]model.DNSRecord, error) {
	return a.store.ListRecords()
}

func (a *App) SaveRecord(r model.DNSRecord) (model.DNSRecord, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return model.DNSRecord{}, fmt.Errorf("record name is required")
	}
	r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
	if r.Type == "" {
		r.Type = "A"
	}
	if r.Type != "A" && r.Type != "AAAA" {
		return model.DNSRecord{}, fmt.Errorf("record type must be A or AAAA")
	}
	// Preferred IP panel always writes DNS-only records.
	r.Proxied = false
	return a.store.UpsertRecord(r)
}

func (a *App) DeleteRecord(id string) error {
	return a.store.DeleteRecord(id)
}

func (a *App) ListTasks(limit int) ([]model.Task, error) {
	return a.store.ListTasks(limit)
}

func (a *App) GetTask(id string) (model.Task, error) {
	return a.store.GetTask(id)
}

func (a *App) ListLogs(limit int, level string) ([]model.LogEntry, error) {
	return a.store.ListLogs(limit, level)
}

func (a *App) ClearLogs() error {
	return a.store.ClearLogs()
}

func (a *App) Status() (model.Status, error) {
	settings, err := a.cfg.Get()
	if err != nil {
		return model.Status{}, err
	}
	bundledPath := ""
	// Keep bundled core available even if files were deleted.
	if bin, err := cfstbin.Ensure(a.dataDir); err == nil {
		bundledPath = bin
		if settings.CFSTBinary == "" || !cfst.BinaryExists(settings.CFSTBinary) {
			settings.CFSTBinary = bin
		}
	}
	count, _ := a.store.CountRecords()
	last, _ := a.store.LatestTask()
	running, _ := a.store.RunningTask()
	st := model.Status{
		Version:         Version,
		UptimeSec:       int64(time.Since(a.startedAt).Seconds()),
		ListenAddr:      firstNonEmpty(a.listenAddr, settings.ListenAddr),
		DataDir:         a.dataDir,
		CFSTBinaryOK:    cfst.BinaryExists(settings.CFSTBinary),
		CFSTBinaryPath:  settings.CFSTBinary,
		CFSTBundled:     cfstbin.SupportsCurrentPlatform() || (bundledPath != "" && settings.CFSTBinary == bundledPath),
		Platform:        cfstbin.PlatformLabel(),
		ScheduleEnabled: settings.ScheduleEnabled,
		CronExpr:        settings.CronExpr,
		LastTask:        last,
		RecordCount:     count,
		Timezone:        settings.Timezone,
	}
	if running != nil {
		st.RunningTaskID = running.ID
	}
	if a.sched != nil {
		st.NextRunAt = a.sched.NextRun()
	}
	return st, nil
}

func (a *App) TestCloudflare(ctx context.Context) (map[string]any, error) {
	settings, err := a.cfg.Get()
	if err != nil {
		return nil, err
	}
	client := cloudflare.New(settings.CFAPIToken, settings.CFZoneID)
	zone, err := client.GetZone(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":        true,
		"zone_id":   zone.ID,
		"zone_name": zone.Name,
	}, nil
}

func (a *App) RunTask(ctx context.Context, trigger model.TaskTrigger) (model.Task, error) {
	a.mu.Lock()
	if a.runningID != "" {
		a.mu.Unlock()
		return model.Task{}, fmt.Errorf("another task is already running: %s", a.runningID)
	}
	task, err := a.store.CreateTask(trigger)
	if err != nil {
		a.mu.Unlock()
		return model.Task{}, err
	}
	// Detach from request-scoped contexts. Manual runs come from HTTP handlers
	// whose context is canceled as soon as the API response is written; using
	// that context would kill CloudflareST immediately after "run" returns.
	_ = ctx
	runCtx, cancel := context.WithCancel(context.Background())
	a.runningID = task.ID
	a.cancelFunc = cancel
	a.mu.Unlock()

	now := time.Now()
	task.Status = model.TaskRunning
	task.StartedAt = &now
	task.Message = "running"
	_ = a.store.UpdateTask(task)
	a.Log(model.LogInfo, "task", fmt.Sprintf("task %s started (%s)", task.ID, trigger))

	go a.executeTask(runCtx, task.ID)
	return task, nil
}

func (a *App) CancelRunning() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelFunc == nil {
		return fmt.Errorf("no running task")
	}
	a.cancelFunc()
	return nil
}

func (a *App) executeTask(ctx context.Context, taskID string) {
	defer func() {
		a.mu.Lock()
		a.runningID = ""
		a.cancelFunc = nil
		a.mu.Unlock()
	}()

	task, err := a.store.GetTask(taskID)
	if err != nil {
		a.Log(model.LogError, "task", err.Error())
		return
	}
	settings, err := a.cfg.Get()
	if err != nil {
		a.failTask(&task, err.Error())
		return
	}

	// Ensure bundled CloudflareST exists before running.
	if bin, err := cfstbin.Ensure(a.dataDir); err == nil {
		if settings.CFSTBinary == "" || !cfst.BinaryExists(settings.CFSTBinary) {
			settings.CFSTBinary = bin
		}
	}
	settings = normalizeIPSettings(settings, a.dataDir)

	var logBuf strings.Builder
	lastPersist := time.Time{}
	persistTask := func(force bool) {
		task.LogText = logBuf.String()
		now := time.Now()
		if !force && !lastPersist.IsZero() && now.Sub(lastPersist) < time.Second {
			return
		}
		lastPersist = now
		_ = a.store.UpdateTask(task)
	}
	appendLog := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		logBuf.WriteString(line)
		logBuf.WriteByte('\n')
		// Keep the latest progress visible on the tasks list.
		if len(line) > 160 {
			task.Message = line[:160] + "..."
		} else {
			task.Message = line
		}
		persistTask(false)
	}

	runCtx, cancel := cfst.WithTimeout(ctx, settings.CFSTTimeoutSec)
	defer cancel()

	versions := []string{settings.CFSTIPVersion}
	if settings.CFSTIPVersion == "both" {
		versions = []string{"4", "6"}
	}

	var allResults []model.SpeedResult
	var selectedList []model.SpeedResult
	totalUpdated := 0
	selectedSummary := make([]string, 0, 8)

	for _, ver := range versions {
		runSettings := settings
		runSettings.CFSTIPVersion = ver
		runSettings = normalizeIPSettings(runSettings, a.dataDir)
		// For dual-stack, force the correct IP list file when not using a remote preferred list.
		if strings.TrimSpace(settings.CFSTIPListURL) == "" {
			if ver == "4" {
				ip4 := filepath.Join(a.dataDir, "ip.txt")
				if _, err := os.Stat(ip4); err == nil {
					runSettings.CFSTIPFile = ip4
				}
			}
			if ver == "6" {
				ip6 := filepath.Join(a.dataDir, "ipv6.txt")
				if _, err := os.Stat(ip6); err == nil {
					runSettings.CFSTIPFile = ip6
				}
			}
		}
		if settings.CFSTIPVersion == "both" {
			if ver == "4" {
				runSettings.CFSTResultFile = filepath.Join(a.dataDir, "result-v4.csv")
			}
			if ver == "6" {
				runSettings.CFSTResultFile = filepath.Join(a.dataDir, "result-v6.csv")
			}
		}

		// Preferred remote candidate list takes priority over local full CF ranges.
		if url := strings.TrimSpace(settings.CFSTIPListURL); url != "" {
			path, count, fetchErr := cfst.FetchPreferredIPList(url, a.dataDir, "v"+ver)
			if fetchErr != nil {
				task.LogText = logBuf.String()
				a.failTask(&task, fmt.Sprintf("IPv%s fetch preferred list failed: %v", ver, fetchErr))
				return
			}
			filteredPath, filteredCount, filterErr := cfst.FilterIPFileByVersion(path, a.dataDir, "v"+ver, ver)
			if filterErr != nil {
				if settings.CFSTIPVersion == "both" {
					appendLog(fmt.Sprintf("skip IPv%s preferred list: %v", ver, filterErr))
					continue
				}
				task.LogText = logBuf.String()
				a.failTask(&task, fmt.Sprintf("IPv%s filter preferred list failed: %v", ver, filterErr))
				return
			}
			runSettings.CFSTIPFile = filteredPath
			appendLog(fmt.Sprintf("=== speed test IPv%s using preferred list url=%s file=%s raw=%d matched=%d ===", ver, url, filteredPath, count, filteredCount))
			// Remote preferred lists are already short; do not sample further.
			settings.CFSTSampleCount = 0
		}

		sampleN := settings.CFSTSampleCount
		if strings.TrimSpace(settings.CFSTIPListURL) == "" {
			ipPath, sampled, sampleErr := cfst.PrepareIPFile(runSettings.CFSTIPFile, a.dataDir, "v"+ver, sampleN)
			if sampleErr != nil {
				task.LogText = logBuf.String()
				a.failTask(&task, fmt.Sprintf("IPv%s prepare ip list failed: %v", ver, sampleErr))
				return
			}
			runSettings.CFSTIPFile = ipPath
			if sampleN > 0 {
				appendLog(fmt.Sprintf("=== speed test IPv%s using sampled list %s (sample=%d, got=%d) ===", ver, ipPath, sampleN, sampled))
			} else {
				appendLog(fmt.Sprintf("=== speed test IPv%s using %s (full list) ===", ver, runSettings.CFSTIPFile))
			}
		}
		appendLog(fmt.Sprintf("note: latency phase uses tiny packets, so bandwidth graph stays near zero until download speed test starts"))

		// Heartbeat so the panel doesn't look frozen during long latency scans.
		hbStop := make(chan struct{})
		startAt := time.Now()
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-hbStop:
					return
				case <-runCtx.Done():
					return
				case now := <-t.C:
					appendLog(fmt.Sprintf("... still running IPv%s, elapsed %ds (CloudflareST alive)", ver, int(now.Sub(startAt).Seconds())))
				}
			}
		}()

		results, output, err := a.runner.Run(runCtx, runSettings, func(line string) {
			appendLog(line)
		})
		close(hbStop)
		if output != "" && !strings.HasSuffix(logBuf.String(), output) {
			appendLog(output)
		}
		if err != nil {
			task.LogText = logBuf.String()
			if runCtx.Err() != nil {
				a.cancelTask(&task, fmt.Sprintf("IPv%s test cancelled or timed out: %v", ver, err))
				return
			}
			a.failTask(&task, fmt.Sprintf("IPv%s test failed: %v", ver, err))
			return
		}
		selected, err := selectTop(results, runSettings)
		if err != nil {
			task.LogText = logBuf.String()
			a.failTask(&task, fmt.Sprintf("IPv%s select failed: %v", ver, err))
			return
		}
		ips := resultIPs(selected)
		primary := selected[0]
		appendLog(fmt.Sprintf("selected IPv%s count=%d ips=%s best(latency=%.2f speed=%.2f loss=%.2f)", ver, len(ips), strings.Join(ips, ", "), primary.Latency, primary.Speed, primary.Loss))
		updated, updateErr := a.applyDNS(runCtx, runSettings, ips, &task, appendLog)
		if updateErr != nil {
			task.LogText = logBuf.String()
			a.failTask(&task, updateErr.Error())
			return
		}
		totalUpdated += updated
		allResults = append(allResults, results...)
		selectedList = append(selectedList, selected...)
		selectedSummary = append(selectedSummary, ips...)
	}

	if len(selectedList) == 0 {
		a.failTask(&task, "no selected ip")
		return
	}
	// Keep primary selected fields from the first successful family (usually v4).
	primary := selectedList[0]
	task.SelectedIP = strings.Join(selectedSummary, ", ")
	task.SelectedLat = primary.Latency
	task.SelectedSpd = primary.Speed
	task.SelectedLoss = primary.Loss
	task.ResultJSON = store.EncodeResults(limitResults(allResults, settings.TopN))
	task.UpdatedCount = totalUpdated

	finished := time.Now()
	task.Status = model.TaskSuccess
	task.FinishedAt = &finished
	task.Message = fmt.Sprintf("selected %s, updated %d record(s)", task.SelectedIP, totalUpdated)
	task.LogText = logBuf.String()
	_ = a.store.UpdateTask(task)
	a.Log(model.LogInfo, "task", task.Message)
	_ = a.notify(settings, task)
}

func (a *App) cancelTask(task *model.Task, message string) {
	finished := time.Now()
	task.Status = model.TaskCancelled
	task.FinishedAt = &finished
	task.Message = message
	_ = a.store.UpdateTask(*task)
	a.Log(model.LogWarn, "task", fmt.Sprintf("task %s cancelled: %s", task.ID, message))
}

func (a *App) failTask(task *model.Task, message string) {
	finished := time.Now()
	task.Status = model.TaskFailed
	task.FinishedAt = &finished
	task.Message = message
	_ = a.store.UpdateTask(*task)
	a.Log(model.LogError, "task", fmt.Sprintf("task %s failed: %s", task.ID, message))
}

func (a *App) applyDNS(ctx context.Context, settings model.Settings, ips []string, task *model.Task, appendLog func(string)) (int, error) {
	records, err := a.store.ListRecords()
	if err != nil {
		return 0, err
	}
	if len(ips) == 0 {
		return 0, fmt.Errorf("no selected ips")
	}
	ipFamily := ipFamilyOf(ips[0])
	var enabled []model.DNSRecord
	for _, r := range records {
		if !r.Enabled {
			continue
		}
		recType := strings.ToUpper(strings.TrimSpace(r.Type))
		if recType == "" {
			recType = "A"
		}
		// Only update records that match the selected IP family.
		if ipFamily == "v6" && recType != "AAAA" {
			appendLog(fmt.Sprintf("skip %s (%s): selected IPv6, record is not AAAA", r.Name, recType))
			continue
		}
		if ipFamily == "v4" && recType != "A" {
			appendLog(fmt.Sprintf("skip %s (%s): selected IPv4, record is not A", r.Name, recType))
			continue
		}
		r.Type = recType
		enabled = append(enabled, r)
	}
	if len(enabled) == 0 {
		return 0, fmt.Errorf("no enabled dns records match selected IP family (%s)", ipFamily)
	}

	joined := strings.Join(ips, ", ")
	if settings.DryRun {
		for _, rec := range enabled {
			appendLog(fmt.Sprintf("dry_run %s %s -> %s", rec.Type, rec.Name, joined))
		}
		appendLog("dry_run enabled, skip dns update")
		return 0, nil
	}
	if settings.CFAPIToken == "" || settings.CFZoneID == "" {
		return 0, fmt.Errorf("cloudflare api token/zone id is required")
	}

	client := cloudflare.New(settings.CFAPIToken, settings.CFZoneID)
	updated := 0
	for _, rec := range enabled {
		current := splitIPs(rec.Content)
		if !settings.ForceUpdate && sameIPSet(current, ips) {
			appendLog(fmt.Sprintf("record %s already points to %s", rec.Name, joined))
			// Still keep panel content tidy.
			rec.Content = joined
			rec.Proxied = false
			if _, err := a.store.UpsertRecord(rec); err != nil {
				return updated, err
			}
			continue
		}
		ttl := rec.TTL
		if ttl <= 0 {
			ttl = settings.CFTTL
		}
		// Preferred-IP workflow must stay DNS-only. Cloudflare proxy would hide/replace the selected IP.
		synced, err := client.SyncRecords(ctx, rec.Type, rec.Name, ips, ttl)
		if err != nil {
			return updated, fmt.Errorf("update %s failed: %w", rec.Name, err)
		}
		actual := make([]string, 0, len(synced))
		for _, item := range synced {
			actual = append(actual, item.Content)
		}
		if len(synced) > 0 {
			rec.CFRecordID = synced[0].ID
			rec.TTL = synced[0].TTL
			rec.Type = synced[0].Type
		}
		if len(actual) > 0 {
			joined = strings.Join(actual, ", ")
		}
		rec.Content = joined
		rec.Proxied = false
		if _, err := a.store.UpsertRecord(rec); err != nil {
			return updated, err
		}
		updated++
		appendLog(fmt.Sprintf("synced %s %s -> %s (requested=%d actual=%d)", rec.Type, rec.Name, joined, len(ips), len(synced)))
		if len(synced) != len(ips) {
			appendLog(fmt.Sprintf("warning: cloudflare returned %d records for %s, expected %d", len(synced), rec.Name, len(ips)))
		}
	}
	return updated, nil
}

func selectTop(results []model.SpeedResult, settings model.Settings) ([]model.SpeedResult, error) {
	var filtered []model.SpeedResult
	for _, r := range results {
		if strings.TrimSpace(r.IP) == "" {
			continue
		}
		if settings.MaxLatencyMS > 0 && r.Latency > float64(settings.MaxLatencyMS) {
			continue
		}
		if settings.MinSpeedMbps > 0 && r.Speed < settings.MinSpeedMbps {
			continue
		}
		if settings.MaxLossPercent > 0 && r.Loss > settings.MaxLossPercent {
			continue
		}
		filtered = append(filtered, r)
	}
	if len(filtered) == 0 {
		filtered = append([]model.SpeedResult{}, results...)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		if settings.PreferLowerLoss && a.Loss != b.Loss {
			return a.Loss < b.Loss
		}
		if a.Speed != b.Speed {
			return a.Speed > b.Speed
		}
		if a.Latency != b.Latency {
			return a.Latency < b.Latency
		}
		return a.IP < b.IP
	})
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no candidates after filtering")
	}

	// Deduplicate by IP while preserving rank order.
	seen := map[string]struct{}{}
	unique := make([]model.SpeedResult, 0, len(filtered))
	for _, r := range filtered {
		ip := strings.TrimSpace(r.IP)
		if ip == "" {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		unique = append(unique, r)
	}
	if len(unique) == 0 {
		return nil, fmt.Errorf("no candidates after filtering")
	}

	n := settings.ResolveIPCount
	if n <= 0 {
		n = 6
	}
	if n > 20 {
		n = 20
	}
	if n > len(unique) {
		n = len(unique)
	}
	return unique[:n], nil
}

func resultIPs(results []model.SpeedResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		ip := strings.TrimSpace(r.IP)
		if ip == "" {
			continue
		}
		out = append(out, ip)
	}
	return out
}

func splitIPs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func sameIPSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := map[string]struct{}{}
	for _, ip := range a {
		left[strings.TrimSpace(ip)] = struct{}{}
	}
	for _, ip := range b {
		if _, ok := left[strings.TrimSpace(ip)]; !ok {
			return false
		}
	}
	return true
}

func limitResults(results []model.SpeedResult, n int) []model.SpeedResult {
	if n <= 0 || n >= len(results) {
		return results
	}
	return results[:n]
}

func (a *App) notify(settings model.Settings, task model.Task) error {
	if !settings.WebhookEnabled || strings.TrimSpace(settings.WebhookURL) == "" {
		return nil
	}
	payload := map[string]any{
		"event":   "task_finished",
		"task":    task,
		"secret":  settings.WebhookSecret,
		"version": Version,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, settings.WebhookURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.Log(model.LogWarn, "webhook", err.Error())
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		a.Log(model.LogWarn, "webhook", fmt.Sprintf("status %d", resp.StatusCode))
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func normalizeIPSettings(settings model.Settings, dataDir string) model.Settings {
	ver := strings.TrimSpace(settings.CFSTIPVersion)
	if ver == "" {
		ver = "4"
	}
	switch ver {
	case "4", "6", "both":
	default:
		ver = "4"
	}
	settings.CFSTIPVersion = ver

	// If user left IP file empty or still on defaults, pick based on version.
	ip4 := filepath.Join(dataDir, "ip.txt")
	ip6 := filepath.Join(dataDir, "ipv6.txt")
	cur := strings.TrimSpace(settings.CFSTIPFile)
	defaultLike := cur == "" || cur == ip4 || cur == "ip.txt" || cur == filepath.Join(dataDir, "ip.txt") || strings.HasSuffix(strings.ReplaceAll(cur, "\\", "/"), "/ip.txt")
	if defaultLike {
		switch ver {
		case "6":
			if _, err := os.Stat(ip6); err == nil {
				settings.CFSTIPFile = ip6
			}
		default:
			if _, err := os.Stat(ip4); err == nil {
				settings.CFSTIPFile = ip4
			}
		}
	}
	return settings
}

func ipFamilyOf(ip string) string {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		// CloudflareST may return IP:port rarely; strip port-like suffix.
		host, _, err := net.SplitHostPort(strings.TrimSpace(ip))
		if err == nil {
			parsed = net.ParseIP(host)
		}
	}
	if parsed == nil {
		if strings.Contains(ip, ":") {
			return "v6"
		}
		return "v4"
	}
	if parsed.To4() != nil {
		return "v4"
	}
	return "v6"
}
