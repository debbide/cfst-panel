package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/debbide/cfst-panel/internal/model"
)

type Runner interface {
	RunTask(ctx context.Context, trigger model.TaskTrigger) (model.Task, error)
	Log(level model.LogLevel, source, message string)
}

type Scheduler struct {
	mu       sync.Mutex
	runner   Runner
	enabled  bool
	expr     string
	timezone string
	stopCh   chan struct{}
	nextRun  *time.Time
}

func New(runner Runner) *Scheduler {
	return &Scheduler{runner: runner}
}

func (s *Scheduler) Reload(settings model.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	s.enabled = settings.ScheduleEnabled
	s.expr = settings.CronExpr
	s.timezone = settings.Timezone
	s.nextRun = nil
	if !settings.ScheduleEnabled {
		return nil
	}
	if _, err := nextFromCron(settings.CronExpr, settings.Timezone, time.Now()); err != nil {
		return err
	}
	s.stopCh = make(chan struct{})
	go s.loop(settings.CronExpr, settings.Timezone, s.stopCh)
	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Scheduler) stopLocked() {
	if s.stopCh != nil {
		close(s.stopCh)
		s.stopCh = nil
	}
	s.enabled = false
	s.nextRun = nil
}

func (s *Scheduler) NextRun() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nextRun == nil {
		return nil
	}
	t := *s.nextRun
	return &t
}

func (s *Scheduler) loop(expr, timezone string, stopCh chan struct{}) {
	for {
		next, err := nextFromCron(expr, timezone, time.Now())
		if err != nil {
			s.runner.Log(model.LogError, "scheduler", err.Error())
			return
		}
		s.mu.Lock()
		s.nextRun = &next
		s.mu.Unlock()

		timer := time.NewTimer(time.Until(next))
		select {
		case <-stopCh:
			timer.Stop()
			return
		case <-timer.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			_, err := s.runner.RunTask(ctx, model.TriggerSchedule)
			cancel()
			if err != nil {
				s.runner.Log(model.LogError, "scheduler", fmt.Sprintf("scheduled task failed: %v", err))
			}
		}
	}
}

// Supports standard 5-field cron: min hour dom mon dow
// Supports */n, lists, ranges, and single numbers. Good enough for panel use.
func nextFromCron(expr, timezone string, from time.Time) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron must have 5 fields: %q", expr)
	}
	loc := time.Local
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	from = from.In(loc).Add(time.Minute).Truncate(time.Minute)
	// Search next 366 days * 24 hours * 60 minutes max.
	for i := 0; i < 366*24*60; i++ {
		t := from.Add(time.Duration(i) * time.Minute)
		if matchCron(fields, t) {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("no next run found for cron %q", expr)
}

func matchCron(fields []string, t time.Time) bool {
	return matchField(fields[0], t.Minute(), 0, 59) &&
		matchField(fields[1], t.Hour(), 0, 23) &&
		matchField(fields[2], t.Day(), 1, 31) &&
		matchField(fields[3], int(t.Month()), 1, 12) &&
		matchField(fields[4], int(t.Weekday()), 0, 6)
}

func matchField(field string, value, min, max int) bool {
	if field == "*" {
		return true
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || step <= 0 {
				continue
			}
			if (value-min)%step == 0 {
				return true
			}
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) != 2 {
				continue
			}
			start, err1 := strconv.Atoi(bounds[0])
			end, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil {
				continue
			}
			if value >= start && value <= end {
				return true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err == nil && n == value {
			return true
		}
	}
	return false
}
