package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// cfDNSRecord is a Cloudflare DNS record (subset of fields).
type cfDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// cfListResult is the Cloudflare API response for list DNS records.
type cfListResult struct {
	Success    bool          `json:"success"`
	Errors     []cfError     `json:"errors"`
	Result     []cfDNSRecord `json:"result"`
	ResultInfo cfResultInfo  `json:"result_info"`
}

// cfSingleResult is for create/update/delete responses.
type cfSingleResult struct {
	Success bool          `json:"success"`
	Errors  []cfError     `json:"errors"`
	Result  cfDNSRecord   `json:"result"`
}

// cfPurgeResult is the cache purge response.
type cfPurgeResult struct {
	Success bool      `json:"success"`
	Errors  []cfError `json:"errors"`
	Result  struct {
		ID string `json:"id"`
	} `json:"result"`
}

// cfAnalyticsResult is a small subset of the analytics response.
type cfAnalyticsResult struct {
	Success bool      `json:"success"`
	Errors  []cfError `json:"errors"`
	Result  struct {
		Totals struct {
			Requests   int `json:"requests"`
			Bandwidth  int `json:"bandwidth"`
			Threats    int `json:"threats"`
			PageViews  int `json:"pageviews"`
			Bytes      int `json:"bytes"`
			CachedRequests int `json:"cachedRequests"`
		} `json:"totals"`
	} `json:"result"`
}

// cfDeleteResult is the delete response (just success/errors).
type cfDeleteResult struct {
	Success bool      `json:"success"`
	Errors  []cfError `json:"errors"`
	Result  struct {
		ID string `json:"id"`
	} `json:"result"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

func (t *turn) cfToken(ctx context.Context) string {
	return t.a.db.GetConfig(ctx, t.projectID, "cf_api_token")
}

func (t *turn) cfZoneID(ctx context.Context) string {
	if z := t.a.db.GetConfig(ctx, t.projectID, "cf_zone_id"); z != "" {
		return z
	}
	return ""
}

func cfReq(ctx context.Context, token, method, path string, body interface{}) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4"+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// cfListDNSRecords lists DNS records, optionally filtered by type.
func (t *turn) cfListDNS(ctx context.Context, recordType string) string {
	token := t.cfToken(ctx)
	zone := t.cfZoneID(ctx)
	if token == "" {
		return "ERROR: cf_api_token is not configured"
	}
	if zone == "" {
		return "ERROR: cf_zone_id is not configured"
	}

	path := fmt.Sprintf("/zones/%s/dns_records?per_page=200", zone)
	if recordType != "" {
		path += "&type=" + recordType
	}

	data, status, err := cfReq(ctx, token, "GET", path, nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: Cloudflare API returned %d: %s", status, truncate(string(data), 400))
	}
	var res cfListResult
	if err := json.Unmarshal(data, &res); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if !res.Success {
		return fmt.Sprintf("ERROR: %v", res.Errors)
	}
	if len(res.Result) == 0 {
		return "(no DNS records found)"
	}
	var b bytes.Buffer
	for _, r := range res.Result {
		proxied := ""
		if r.Proxied {
			proxied = " 🟠"
		}
		fmt.Fprintf(&b, "%s  %s  %-6s  %s%s\n", r.ID, r.Name, r.Type, r.Content, proxied)
	}
	fmt.Fprintf(&b, "total: %d", res.ResultInfo.TotalCount)
	return b.String()
}

// cfCreateDNS creates a new DNS record.
func (t *turn) cfCreateDNS(ctx context.Context, recordType, name, content string, ttl int, proxied bool) string {
	token := t.cfToken(ctx)
	zone := t.cfZoneID(ctx)
	if token == "" {
		return "ERROR: cf_api_token is not configured"
	}
	if zone == "" {
		return "ERROR: cf_zone_id is not configured"
	}

	payload := map[string]interface{}{
		"type":    recordType,
		"name":    name,
		"content": content,
		"ttl":     ttl,
		"proxied": proxied,
	}

	data, status, err := cfReq(ctx, token, "POST", fmt.Sprintf("/zones/%s/dns_records", zone), payload)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: Cloudflare API returned %d: %s", status, truncate(string(data), 400))
	}
	var res cfSingleResult
	if err := json.Unmarshal(data, &res); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if !res.Success {
		return fmt.Sprintf("ERROR: %v", res.Errors)
	}
	proxiedStr := ""
	if proxied {
		proxiedStr = " (proxied)"
	}
	return fmt.Sprintf("created %s record: %s → %s %s ttl=%d%s", recordType, name, content, res.Result.ID, ttl, proxiedStr)
}

// cfUpdateDNS updates an existing DNS record's name, content, and proxied status.
func (t *turn) cfUpdateDNS(ctx context.Context, recordID, recordType, name, content string, proxied bool) string {
	token := t.cfToken(ctx)
	zone := t.cfZoneID(ctx)
	if token == "" {
		return "ERROR: cf_api_token is not configured"
	}
	if zone == "" {
		return "ERROR: cf_zone_id is not configured"
	}

	payload := map[string]interface{}{
		"type":    recordType,
		"name":    name,
		"content": content,
		"proxied": proxied,
	}

	data, status, err := cfReq(ctx, token, "PUT", fmt.Sprintf("/zones/%s/dns_records/%s", zone, recordID), payload)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: Cloudflare API returned %d: %s", status, truncate(string(data), 400))
	}
	var res cfSingleResult
	if err := json.Unmarshal(data, &res); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if !res.Success {
		return fmt.Sprintf("ERROR: %v", res.Errors)
	}
	proxiedStr := ""
	if proxied {
		proxiedStr = " (proxied)"
	}
	return fmt.Sprintf("updated record %s: %s → %s%s", recordID, res.Result.Name, res.Result.Content, proxiedStr)
}

// cfDeleteDNS deletes a DNS record by ID.
func (t *turn) cfDeleteDNS(ctx context.Context, recordID string) string {
	token := t.cfToken(ctx)
	zone := t.cfZoneID(ctx)
	if token == "" {
		return "ERROR: cf_api_token is not configured"
	}
	if zone == "" {
		return "ERROR: cf_zone_id is not configured"
	}

	data, status, err := cfReq(ctx, token, "DELETE", fmt.Sprintf("/zones/%s/dns_records/%s", zone, recordID), nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: Cloudflare API returned %d: %s", status, truncate(string(data), 400))
	}
	var res cfDeleteResult
	if err := json.Unmarshal(data, &res); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if !res.Success {
		return fmt.Sprintf("ERROR: %v", res.Errors)
	}
	return fmt.Sprintf("deleted DNS record %s", recordID)
}

// cfPurgeCache purges cache for specified files or everything.
func (t *turn) cfPurgeCache(ctx context.Context, files string) string {
	token := t.cfToken(ctx)
	zone := t.cfZoneID(ctx)
	if token == "" {
		return "ERROR: cf_api_token is not configured"
	}
	if zone == "" {
		return "ERROR: cf_zone_id is not configured"
	}

	payload := map[string]interface{}{}
	if files == "*" || files == "everything" {
		payload["purge_everything"] = true
	} else {
		// Split on comma or whitespace
		fileList := splitList(files)
		payload["files"] = fileList
	}

	data, status, err := cfReq(ctx, token, "POST", fmt.Sprintf("/zones/%s/purge_cache", zone), payload)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: Cloudflare API returned %d: %s", status, truncate(string(data), 400))
	}
	var res cfPurgeResult
	if err := json.Unmarshal(data, &res); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if !res.Success {
		return fmt.Sprintf("ERROR: %v", res.Errors)
	}
	if files == "*" || files == "everything" {
		return "purged entire cache"
	}
	return fmt.Sprintf("purged %d URL(s)", len(splitList(files)))
}

// cfGetAnalytics returns basic zone analytics for the given time window.
func (t *turn) cfGetAnalytics(ctx context.Context, sinceHours int) string {
	token := t.cfToken(ctx)
	zone := t.cfZoneID(ctx)
	if token == "" {
		return "ERROR: cf_api_token is not configured"
	}
	if zone == "" {
		return "ERROR: cf_zone_id is not configured"
	}
	if sinceHours <= 0 {
		sinceHours = 24
	}
	if sinceHours > 72 {
		return "ERROR: analytics window is limited to 72 hours"
	}

	since := time.Now().Add(-time.Duration(sinceHours) * time.Hour).UTC().Format(time.RFC3339)
	until := time.Now().UTC().Format(time.RFC3339)

	path := fmt.Sprintf("/zones/%s/analytics/dashboard?since=%s&until=%s", zone, since, until)
	data, status, err := cfReq(ctx, token, "GET", path, nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: Cloudflare API returned %d: %s", status, truncate(string(data), 400))
	}
	var res cfAnalyticsResult
	if err := json.Unmarshal(data, &res); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if !res.Success {
		return fmt.Sprintf("ERROR: %v", res.Errors)
	}
	totals := res.Result.Totals
	mb := float64(totals.Bandwidth) / 1048576.0
	return fmt.Sprintf(
		"last %dh — requests: %d, page views: %d, bytes: %d (%.1f MB), cached requests: %d, threats: %d",
		sinceHours, totals.Requests, totals.PageViews, totals.Bytes, mb, totals.CachedRequests, totals.Threats,
	)
}

func splitList(s string) []string {
	f := func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' }
	return strings.FieldsFunc(s, f)
}
