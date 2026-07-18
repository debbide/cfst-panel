package cfst

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/debbide/cfst-panel/internal/model"
)

type Runner struct{}

func NewRunner() *Runner { return &Runner{} }

func (r *Runner) BuildArgs(settings model.Settings) []string {
	args := []string{}
	if settings.CFSTIPFile != "" {
		args = append(args, "-f", settings.CFSTIPFile)
	}
	if settings.CFSTThreads > 0 {
		args = append(args, "-n", strconv.Itoa(settings.CFSTThreads))
	}
	if settings.CFSTDelay > 0 {
		args = append(args, "-t", strconv.Itoa(settings.CFSTDelay))
	}
	if settings.CFSTTestCount > 0 {
		args = append(args, "-dn", strconv.Itoa(settings.CFSTTestCount))
	}
	if settings.CFSTPort > 0 {
		args = append(args, "-tp", strconv.Itoa(settings.CFSTPort))
	}
	if settings.CFSTSpeedTestURL != "" {
		args = append(args, "-url", settings.CFSTSpeedTestURL)
	}
	if settings.CFSTEnableHttping {
		args = append(args, "-httping")
	}
	if settings.MaxLatencyMS > 0 {
		args = append(args, "-tl", strconv.Itoa(settings.MaxLatencyMS))
	}
	if settings.MinSpeedMbps > 0 {
		args = append(args, "-sl", fmt.Sprintf("%.2f", settings.MinSpeedMbps))
	}
	if settings.CFSTResultFile != "" {
		args = append(args, "-o", settings.CFSTResultFile)
	}
	extra := strings.Fields(settings.CFSTExtraArgs)
	args = append(args, extra...)
	return args
}

func (r *Runner) Run(ctx context.Context, settings model.Settings, onLine func(string)) ([]model.SpeedResult, string, error) {
	binary := settings.CFSTBinary
	if binary == "" {
		return nil, "", fmt.Errorf("cfst binary path is empty")
	}
	if _, err := os.Stat(binary); err != nil {
		return nil, "", fmt.Errorf("cfst binary not found: %s", binary)
	}
	if err := os.MkdirAll(settings.CFSTWorkingDir, 0o755); err != nil {
		return nil, "", err
	}
	if settings.CFSTResultFile != "" {
		_ = os.MkdirAll(filepath.Dir(settings.CFSTResultFile), 0o755)
	}

	args := r.BuildArgs(settings)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = settings.CFSTWorkingDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", err
	}

	var logBuilder strings.Builder
	writeLine := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		logBuilder.WriteString(line)
		logBuilder.WriteByte('\n')
		if onLine != nil {
			onLine(line)
		}
	}

	writeLine(fmt.Sprintf("exec: %s %s", binary, strings.Join(args, " ")))
	writeLine(fmt.Sprintf("workdir: %s", settings.CFSTWorkingDir))

	if err := cmd.Start(); err != nil {
		return nil, "", err
	}

	done := make(chan error, 2)
	go streamProgress(stdout, writeLine, done)
	go streamProgress(stderr, writeLine, done)

	waitErr := cmd.Wait()
	<-done
	<-done

	results, parseErr := ParseResultCSV(settings.CFSTResultFile)
	if waitErr != nil {
		if ctx.Err() != nil {
			return results, logBuilder.String(), fmt.Errorf("cfst cancelled or timed out: %w", ctx.Err())
		}
		return results, logBuilder.String(), fmt.Errorf("cfst failed: %w", waitErr)
	}
	if parseErr != nil {
		return results, logBuilder.String(), parseErr
	}
	if len(results) == 0 {
		return results, logBuilder.String(), fmt.Errorf("no speed results parsed from %s", settings.CFSTResultFile)
	}
	return results, logBuilder.String(), nil
}

// streamProgress reads CloudflareST output and emits on both '\n' and '\r'.
// Latency/speed stages often rewrite the same line with carriage returns, so a
// newline-only scanner looks frozen for a long time.
func streamProgress(r io.Reader, onLine func(string), done chan<- error) {
	br := bufio.NewReaderSize(r, 1024*1024)
	var pending strings.Builder
	flush := func() {
		if pending.Len() == 0 {
			return
		}
		line := strings.TrimRight(pending.String(), " \t\r\n")
		pending.Reset()
		if line != "" {
			onLine(line)
		}
	}
	for {
		b, err := br.ReadByte()
		if err != nil {
			flush()
			if err == io.EOF {
				done <- nil
				return
			}
			done <- err
			return
		}
		if b == '\n' || b == '\r' {
			flush()
			continue
		}
		pending.WriteByte(b)
	}
}

func ParseResultCSV(path string) ([]model.SpeedResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open result csv: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse result csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	start := 0
	// Skip header if present.
	if looksLikeHeader(rows[0]) {
		start = 1
	}
	var out []model.SpeedResult
	for i := start; i < len(rows); i++ {
		row := rows[i]
		if len(row) == 0 {
			continue
		}
		ip := strings.TrimSpace(row[0])
		if ip == "" {
			continue
		}
		item := model.SpeedResult{IP: ip, Raw: strings.Join(row, ",")}
		// CloudflareST common columns:
		// IP, 已发送, 已接收, 丢包率, 平均延迟, 下载速度(MB/s)
		if len(row) >= 5 {
			item.Loss = parseFloat(row[3])
			item.Latency = parseFloat(row[4])
		}
		if len(row) >= 6 {
			item.Speed = parseFloat(row[5])
		}
		// Fallback positions for alternate exports.
		if item.Latency == 0 && len(row) >= 2 {
			item.Latency = parseFloat(row[1])
		}
		if item.Speed == 0 && len(row) >= 3 {
			item.Speed = parseFloat(row[2])
		}
		out = append(out, item)
	}
	return out, nil
}

func looksLikeHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	first := strings.ToLower(strings.TrimSpace(row[0]))
	return strings.Contains(first, "ip") || strings.Contains(first, "地址")
}

func parseFloat(v string) float64 {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, "%")
	v = strings.TrimSuffix(v, "ms")
	v = strings.TrimSuffix(v, "MB/s")
	v = strings.TrimSpace(v)
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

func BinaryExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func WithTimeout(parent context.Context, timeoutSec int) (context.Context, context.CancelFunc) {
	if timeoutSec <= 0 {
		timeoutSec = 600
	}
	return context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
}
