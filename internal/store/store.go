package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/debbide/cfst-panel/internal/model"
)

var ErrNotFoundError = errors.New("not found")

type Store struct {
	mu      sync.Mutex
	dir     string
	meta    map[string]string
	records map[string]model.DNSRecord
	tasks   map[string]model.Task
	logs    []model.LogEntry
	sess    map[string]session
	logSeq  int64
}

type session struct {
	User      string    `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type snapshot struct {
	Meta    map[string]string           `json:"meta"`
	Records map[string]model.DNSRecord  `json:"records"`
	Tasks   map[string]model.Task       `json:"tasks"`
	Logs    []model.LogEntry            `json:"logs"`
	Sess    map[string]session          `json:"sessions"`
	LogSeq  int64                       `json:"log_seq"`
}

func Open(path string) (*Store, error) {
	// Accept either a data directory or a file path (panel.json / panel.db).
	dir := path
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".db") {
		dir = filepath.Dir(path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:     dir,
		meta:    map[string]string{},
		records: map[string]model.DNSRecord{},
		tasks:   map[string]model.Task{},
		logs:    []model.LogEntry{},
		sess:    map[string]session{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) filePath() string {
	return filepath.Join(s.dir, "panel.json")
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return s.persistLocked()
		}
		return err
	}
	var snap snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	if snap.Meta == nil {
		snap.Meta = map[string]string{}
	}
	if snap.Records == nil {
		snap.Records = map[string]model.DNSRecord{}
	}
	if snap.Tasks == nil {
		snap.Tasks = map[string]model.Task{}
	}
	if snap.Sess == nil {
		snap.Sess = map[string]session{}
	}
	if snap.Logs == nil {
		snap.Logs = []model.LogEntry{}
	}
	s.meta = snap.Meta
	s.records = snap.Records
	s.tasks = snap.Tasks
	s.logs = snap.Logs
	s.sess = snap.Sess
	s.logSeq = snap.LogSeq
	return nil
}

func (s *Store) persistLocked() error {
	snap := snapshot{
		Meta:    s.meta,
		Records: s.records,
		Tasks:   s.tasks,
		Logs:    s.logs,
		Sess:    s.sess,
		LogSeq:  s.logSeq,
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath())
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Store) GetMeta(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta[key], nil
}

func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta[key] = value
	return s.persistLocked()
}

func (s *Store) CreateSession(user string, hours int) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := newID()
	now := time.Now()
	exp := now.Add(time.Duration(hours) * time.Hour)
	s.sess[token] = session{User: user, ExpiresAt: exp, CreatedAt: now}
	return token, exp, s.persistLocked()
}

func (s *Store) GetSession(token string) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.sess[token]
	if !ok {
		return "", time.Time{}, ErrNotFoundError
	}
	if time.Now().After(ss.ExpiresAt) {
		delete(s.sess, token)
		_ = s.persistLocked()
		return "", time.Time{}, ErrNotFoundError
	}
	return ss.User, ss.ExpiresAt, nil
}

func (s *Store) DeleteSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sess, token)
	return s.persistLocked()
}

func (s *Store) ListRecords() ([]model.DNSRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.DNSRecord, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) GetRecord(id string) (model.DNSRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return model.DNSRecord{}, ErrNotFoundError
	}
	return r, nil
}

func (s *Store) UpsertRecord(r model.DNSRecord) (model.DNSRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if r.ID == "" {
		r.ID = newID()
		r.CreatedAt = now
	} else if old, ok := s.records[r.ID]; ok && r.CreatedAt.IsZero() {
		r.CreatedAt = old.CreatedAt
	} else if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	if r.Type == "" {
		r.Type = "A"
	}
	if r.TTL <= 0 {
		r.TTL = 1
	}
	s.records[r.ID] = r
	return r, s.persistLocked()
}

func (s *Store) DeleteRecord(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return s.persistLocked()
}

func (s *Store) CreateTask(trigger model.TaskTrigger) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	t := model.Task{
		ID:        newID(),
		Status:    model.TaskPending,
		Trigger:   trigger,
		CreatedAt: now,
	}
	s.tasks[t.ID] = t
	return t, s.persistLocked()
}

func (s *Store) UpdateTask(t model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
	return s.persistLocked()
}

func (s *Store) GetTask(id string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return model.Task{}, ErrNotFoundError
	}
	return t, nil
}

func (s *Store) ListTasks(limit int) ([]model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}
	out := make([]model.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) LatestTask() (*model.Task, error) {
	tasks, err := s.ListTasks(1)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	return &tasks[0], nil
}

func (s *Store) RunningTask() (*model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *model.Task
	for _, t := range s.tasks {
		if t.Status != model.TaskRunning {
			continue
		}
		tt := t
		if latest == nil || tt.CreatedAt.After(latest.CreatedAt) {
			latest = &tt
		}
	}
	return latest, nil
}

func (s *Store) AddLog(level model.LogLevel, source, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logSeq++
	s.logs = append(s.logs, model.LogEntry{
		ID:        s.logSeq,
		Level:     level,
		Source:    source,
		Message:   message,
		CreatedAt: time.Now(),
	})
	if len(s.logs) > 2000 {
		s.logs = s.logs[len(s.logs)-2000:]
	}
	return s.persistLocked()
}

func (s *Store) ListLogs(limit int, level string) ([]model.LogEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 200
	}
	out := make([]model.LogEntry, 0, len(s.logs))
	for i := len(s.logs) - 1; i >= 0; i-- {
		e := s.logs[i]
		if level != "" && string(e.Level) != level {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Store) ClearLogs() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = []model.LogEntry{}
	return s.persistLocked()
}

func (s *Store) CountRecords() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records), nil
}

func EncodeResults(results []model.SpeedResult) string {
	b, _ := json.Marshal(results)
	return string(b)
}

func DecodeResults(raw string) []model.SpeedResult {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []model.SpeedResult
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func ErrNotFound(err error) bool {
	return errors.Is(err, ErrNotFoundError)
}

func Wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w", err)
}
