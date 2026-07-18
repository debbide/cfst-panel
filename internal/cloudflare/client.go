package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	token      string
	zoneID     string
	httpClient *http.Client
}

func New(token, zoneID string) *Client {
	return &Client{
		token:  strings.TrimSpace(token),
		zoneID: strings.TrimSpace(zoneID),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type apiResponse struct {
	Success bool            `json:"success"`
	Errors  []apiError      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) Validate(ctx context.Context) error {
	if c.token == "" {
		return fmt.Errorf("cloudflare api token is empty")
	}
	if c.zoneID == "" {
		return fmt.Errorf("cloudflare zone id is empty")
	}
	_, err := c.GetZone(ctx)
	return err
}

func (c *Client) GetZone(ctx context.Context) (Zone, error) {
	var zone Zone
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s", c.zoneID), nil, &zone)
	return zone, err
}

func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	var zones []Zone
	err := c.do(ctx, http.MethodGet, "/zones?per_page=50", nil, &zones)
	return zones, err
}

func (c *Client) FindRecord(ctx context.Context, name, recordType string) (*DNSRecord, error) {
	records, err := c.ListRecords(ctx, name, recordType)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	return &records[0], nil
}

func (c *Client) ListRecords(ctx context.Context, name, recordType string) ([]DNSRecord, error) {
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	q := url.Values{}
	if recordType != "" {
		q.Set("type", recordType)
	}
	if strings.TrimSpace(name) != "" {
		q.Set("name", strings.TrimSpace(name))
	}
	q.Set("per_page", "100")
	path := fmt.Sprintf("/zones/%s/dns_records?%s", c.zoneID, q.Encode())
	var records []DNSRecord
	if err := c.do(ctx, http.MethodGet, path, nil, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (c *Client) DeleteRecord(ctx context.Context, recordID string) error {
	recordID = strings.TrimSpace(recordID)
	if recordID == "" {
		return fmt.Errorf("record id is empty")
	}
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/zones/%s/dns_records/%s", c.zoneID, recordID), nil, nil)
}

func (c *Client) UpsertARecord(ctx context.Context, name, content string, ttl int, proxied bool, existingID string) (DNSRecord, error) {
	return c.UpsertRecord(ctx, "A", name, content, ttl, proxied, existingID)
}

func (c *Client) UpsertRecord(ctx context.Context, recordType, name, content string, ttl int, proxied bool, existingID string) (DNSRecord, error) {
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	if recordType == "" {
		recordType = "A"
	}
	if recordType != "A" && recordType != "AAAA" {
		return DNSRecord{}, fmt.Errorf("unsupported dns record type: %s", recordType)
	}
	payload := map[string]any{
		"type":    recordType,
		"name":    name,
		"content": content,
		"ttl":     ttl,
		"proxied": proxied,
	}
	if existingID != "" {
		var updated DNSRecord
		err := c.do(ctx, http.MethodPut, fmt.Sprintf("/zones/%s/dns_records/%s", c.zoneID, existingID), payload, &updated)
		return updated, err
	}
	found, err := c.FindRecord(ctx, name, recordType)
	if err != nil {
		return DNSRecord{}, err
	}
	if found != nil {
		var updated DNSRecord
		err := c.do(ctx, http.MethodPut, fmt.Sprintf("/zones/%s/dns_records/%s", c.zoneID, found.ID), payload, &updated)
		return updated, err
	}
	var created DNSRecord
	err = c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", c.zoneID), payload, &created)
	return created, err
}

// SyncRecords makes sure name/type exactly resolves to desiredIPs (DNS-only).
// Extra existing records of the same name/type are deleted.
// Important: missing IPs must be created with POST. Never call Upsert without an
// existing record ID here, because that would overwrite the first record and leave only 1 IP.
func (c *Client) SyncRecords(ctx context.Context, recordType, name string, desiredIPs []string, ttl int) ([]DNSRecord, error) {
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	if recordType == "" {
		recordType = "A"
	}
	if recordType != "A" && recordType != "AAAA" {
		return nil, fmt.Errorf("unsupported dns record type: %s", recordType)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("record name is empty")
	}
	if ttl <= 0 {
		ttl = 1
	}

	// de-dup desired while preserving order
	seen := map[string]struct{}{}
	var ips []string
	for _, ip := range desiredIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		ips = append(ips, ip)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no desired ips")
	}

	existing, err := c.ListRecords(ctx, name, recordType)
	if err != nil {
		return nil, err
	}

	// map content -> records (may have duplicates)
	byIP := map[string][]DNSRecord{}
	for _, rec := range existing {
		byIP[rec.Content] = append(byIP[rec.Content], rec)
	}

	desiredSet := map[string]struct{}{}
	for _, ip := range ips {
		desiredSet[ip] = struct{}{}
	}

	// Keep/update one record for each desired IP that already exists.
	keptIDs := map[string]struct{}{}
	for _, ip := range ips {
		list := byIP[ip]
		if len(list) == 0 {
			continue
		}
		rec := list[0]
		if rec.Proxied || (ttl > 0 && rec.TTL != ttl) {
			updated, err := c.UpsertRecord(ctx, recordType, name, ip, ttl, false, rec.ID)
			if err != nil {
				return nil, fmt.Errorf("update existing %s %s -> %s failed: %w", recordType, name, ip, err)
			}
			rec = updated
		}
		keptIDs[rec.ID] = struct{}{}
		// delete duplicate records of the same IP later
	}

	// Create missing desired IPs with explicit POST only.
	for _, ip := range ips {
		if list := byIP[ip]; len(list) > 0 {
			continue
		}
		created, err := c.createRecord(ctx, recordType, name, ip, ttl, false)
		if err != nil {
			return nil, fmt.Errorf("create %s %s -> %s failed: %w", recordType, name, ip, err)
		}
		if strings.TrimSpace(created.Content) != ip {
			return nil, fmt.Errorf("create %s %s expected %s but got %s", recordType, name, ip, created.Content)
		}
		keptIDs[created.ID] = struct{}{}
		byIP[ip] = append(byIP[ip], created)
	}

	// Refresh and delete extras / duplicates not needed.
	existing, err = c.ListRecords(ctx, name, recordType)
	if err != nil {
		return nil, err
	}
	have := map[string]struct{}{}
	for _, rec := range existing {
		if _, ok := desiredSet[rec.Content]; !ok {
			_ = c.DeleteRecord(ctx, rec.ID)
			continue
		}
		if _, ok := have[rec.Content]; ok {
			_ = c.DeleteRecord(ctx, rec.ID)
			continue
		}
		if rec.Proxied || (ttl > 0 && rec.TTL != ttl) {
			updated, err := c.UpsertRecord(ctx, recordType, name, rec.Content, ttl, false, rec.ID)
			if err == nil {
				rec = updated
			}
		}
		have[rec.Content] = struct{}{}
	}

	// final list in desired order
	existing, err = c.ListRecords(ctx, name, recordType)
	if err != nil {
		return nil, err
	}
	byContent := map[string]DNSRecord{}
	for _, rec := range existing {
		// keep first occurrence per content
		if _, ok := byContent[rec.Content]; !ok {
			byContent[rec.Content] = rec
		}
	}
	out := make([]DNSRecord, 0, len(ips))
	missing := make([]string, 0)
	for _, ip := range ips {
		if rec, ok := byContent[ip]; ok {
			out = append(out, rec)
		} else {
			missing = append(missing, ip)
		}
	}
	if len(missing) > 0 {
		return out, fmt.Errorf("sync incomplete for %s %s, missing: %s", recordType, name, strings.Join(missing, ", "))
	}
	return out, nil
}

func (c *Client) createRecord(ctx context.Context, recordType, name, content string, ttl int, proxied bool) (DNSRecord, error) {
	payload := map[string]any{
		"type":    recordType,
		"name":    name,
		"content": content,
		"ttl":     ttl,
		"proxied": proxied,
	}
	var created DNSRecord
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", c.zoneID), payload, &created)
	return created, err
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4"+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var api apiResponse
	if err := json.Unmarshal(raw, &api); err != nil {
		return fmt.Errorf("cloudflare response decode failed: %w, body=%s", err, string(raw))
	}
	if !api.Success {
		if len(api.Errors) > 0 {
			return fmt.Errorf("cloudflare api error: %s", api.Errors[0].Message)
		}
		return fmt.Errorf("cloudflare api failed: %s", string(raw))
	}
	if out == nil {
		return nil
	}
	if len(api.Result) == 0 || string(api.Result) == "null" {
		return nil
	}
	return json.Unmarshal(api.Result, out)
}
