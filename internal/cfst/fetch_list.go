package cfst

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FetchPreferredIPList downloads a remote preferred-IP list and writes a clean
// one-IP-per-line file under dataDir. Supports formats like:
//
//	1.1.1.1
//	1.1.1.1#remark
//	1.1.1.1,1.0.0.1
func FetchPreferredIPList(listURL, dataDir, tag string) (path string, count int, err error) {
	listURL = strings.TrimSpace(listURL)
	if listURL == "" {
		return "", 0, fmt.Errorf("ip list url is empty")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", 0, err
	}
	if tag == "" {
		tag = "v4"
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, listURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "cfst-panel/0.3")
	req.Header.Set("Accept", "text/plain,text/*,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("download preferred ip list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", 0, fmt.Errorf("download preferred ip list: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	ips, err := parsePreferredIPBody(resp.Body)
	if err != nil {
		return "", 0, err
	}
	if len(ips) == 0 {
		return "", 0, fmt.Errorf("preferred ip list is empty: %s", listURL)
	}

	path = filepath.Join(dataDir, fmt.Sprintf("preferred-%s.txt", tag))
	if err := writeLines(path, ips); err != nil {
		return "", 0, err
	}
	return path, len(ips), nil
}

func parsePreferredIPBody(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 2*1024*1024)

	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		// strip remark: ip#note or ip // note
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		if i := strings.Index(raw, "//"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		raw = strings.Trim(raw, ",;|")
		if raw == "" || strings.HasPrefix(raw, "#") {
			return
		}
		// allow host:port
		host := raw
		if h, _, err := net.SplitHostPort(raw); err == nil {
			host = h
		}
		// CIDR keep as-is if valid
		if strings.Contains(host, "/") {
			if _, _, err := net.ParseCIDR(host); err == nil {
				if _, ok := seen[host]; ok {
					return
				}
				seen[host] = struct{}{}
				out = append(out, host)
			}
			return
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return
		}
		s := ip.String()
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// support comma/space separated on one line
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '|' || r == ' ' || r == '\t'
		})
		if len(fields) == 0 {
			add(line)
			continue
		}
		for _, f := range fields {
			add(f)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FilterIPFileByVersion keeps only IPv4 or IPv6 entries from an existing list file.
func FilterIPFileByVersion(sourcePath, dataDir, tag, version string) (string, int, error) {
	entries, err := readIPEntries(sourcePath)
	if err != nil {
		return "", 0, err
	}
	wantV6 := version == "6"
	var lines []string
	for _, e := range entries {
		ip := e.ip
		if e.ipnet != nil {
			ip = e.ipnet.IP
		}
		if ip == nil {
			continue
		}
		isV4 := ip.To4() != nil
		if wantV6 && isV4 {
			continue
		}
		if !wantV6 && !isV4 {
			continue
		}
		if e.ipnet != nil {
			lines = append(lines, e.raw)
		} else {
			lines = append(lines, ip.String())
		}
	}
	if len(lines) == 0 {
		return "", 0, fmt.Errorf("preferred list has no IPv%s addresses", version)
	}
	if tag == "" {
		tag = "v" + version
	}
	out := filepath.Join(dataDir, fmt.Sprintf("preferred-filtered-%s.txt", tag))
	if err := writeLines(out, lines); err != nil {
		return "", 0, err
	}
	return out, len(lines), nil
}