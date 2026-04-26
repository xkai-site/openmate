package httpapi

import (
	"bufio"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"vos/internal/vos/domain"
)

type toolMonitorEvent struct {
	EventID    string  `json:"event_id"`
	Phase      string  `json:"phase"`
	TS         string  `json:"ts"`
	NodeID     string  `json:"node_id,omitempty"`
	ToolName   string  `json:"tool_name,omitempty"`
	Source     string  `json:"source,omitempty"`
	IsSafe     bool    `json:"is_safe"`
	IsReadOnly bool    `json:"is_read_only"`
	RequestID  *string `json:"request_id,omitempty"`
	Success    *bool   `json:"success,omitempty"`
	ErrorCode  *string `json:"error_code,omitempty"`
	DurationMS *int    `json:"duration_ms,omitempty"`
}

type toolMonitorQuery struct {
	ToolName      string
	NodeID        string
	Source        string
	Success       *bool
	Limit         int
	WindowMinutes int
}

type toolMonitorSummaryItem struct {
	ToolName      string  `json:"tool_name"`
	Count         int     `json:"count"`
	SuccessRate   float64 `json:"success_rate"`
	AvgDurationMS float64 `json:"avg_duration_ms"`
	P95DurationMS float64 `json:"p95_duration_ms"`
}

func (server *Server) handleV1ToolMonitorRoutes(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, v1Prefix+"/tools/monitor")
	switch path {
	case "/events":
		server.handleV1ToolMonitorEvents(writer, request)
	case "/summary":
		server.handleV1ToolMonitorSummary(writer, request)
	default:
		server.writeV1Error(writer, http.StatusNotFound, "not found")
	}
}

func (server *Server) handleV1ToolMonitorEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}
	query, err := parseToolMonitorQuery(request)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	events, err := server.loadToolMonitorEvents()
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	filtered := filterToolMonitorEvents(events, query, false)
	server.writeV1Success(writer, filtered)
}

func (server *Server) handleV1ToolMonitorSummary(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodGet)
		return
	}
	query, err := parseToolMonitorQuery(request)
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	events, err := server.loadToolMonitorEvents()
	if err != nil {
		server.writeV1ServiceError(writer, err)
		return
	}
	filtered := filterToolMonitorEvents(events, query, true)
	summary := summarizeToolMonitorEvents(filtered, query.Limit)
	server.writeV1Success(writer, summary)
}

func parseToolMonitorQuery(request *http.Request) (toolMonitorQuery, error) {
	values := request.URL.Query()
	query := toolMonitorQuery{
		ToolName: strings.TrimSpace(values.Get("tool_name")),
		NodeID:   strings.TrimSpace(values.Get("node_id")),
		Source:   strings.TrimSpace(values.Get("source")),
		Limit:    100,
	}

	if query.Source != "" {
		switch query.Source {
		case "model", "cli", "http", "unknown":
		default:
			return toolMonitorQuery{}, domain.ValidationError{Message: "source must be one of model|cli|http|unknown"}
		}
	}

	if raw := strings.TrimSpace(values.Get("success")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return toolMonitorQuery{}, domain.ValidationError{Message: "success must be true or false"}
		}
		query.Success = &parsed
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return toolMonitorQuery{}, domain.ValidationError{Message: "limit must be a positive integer"}
		}
		query.Limit = parsed
	}
	if raw := strings.TrimSpace(values.Get("window_minutes")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return toolMonitorQuery{}, domain.ValidationError{Message: "window_minutes must be a positive integer"}
		}
		query.WindowMinutes = parsed
	}
	return query, nil
}

func (server *Server) loadToolMonitorEvents() ([]toolMonitorEvent, error) {
	path := filepath.Join(server.workspace, filepath.FromSlash(".openmate/runtime/tool_monitor.jsonl"))
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []toolMonitorEvent{}, nil
		}
		return nil, err
	}
	defer file.Close()

	events := []toolMonitorEvent{}
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event toolMonitorEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func filterToolMonitorEvents(events []toolMonitorEvent, query toolMonitorQuery, afterOnly bool) []toolMonitorEvent {
	cutoff := time.Time{}
	if query.WindowMinutes > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(query.WindowMinutes) * time.Minute)
	}

	filtered := make([]toolMonitorEvent, 0, len(events))
	for _, event := range events {
		if afterOnly && event.Phase != "after" {
			continue
		}
		if query.ToolName != "" && event.ToolName != query.ToolName {
			continue
		}
		if query.NodeID != "" && event.NodeID != query.NodeID {
			continue
		}
		if query.Source != "" && event.Source != query.Source {
			continue
		}
		if query.Success != nil {
			if event.Phase != "after" || event.Success == nil || *event.Success != *query.Success {
				continue
			}
		}
		if !cutoff.IsZero() {
			parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(event.TS))
			if err != nil || parsed.Before(cutoff) {
				continue
			}
		}
		filtered = append(filtered, event)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(filtered[i].TS))
		right, rightErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(filtered[j].TS))
		if leftErr == nil && rightErr == nil {
			return left.After(right)
		}
		return filtered[i].TS > filtered[j].TS
	})

	if query.Limit > 0 && len(filtered) > query.Limit {
		return filtered[:query.Limit]
	}
	return filtered
}

func summarizeToolMonitorEvents(events []toolMonitorEvent, limit int) []toolMonitorSummaryItem {
	grouped := map[string][]toolMonitorEvent{}
	for _, event := range events {
		if event.Phase != "after" {
			continue
		}
		grouped[event.ToolName] = append(grouped[event.ToolName], event)
	}

	summary := make([]toolMonitorSummaryItem, 0, len(grouped))
	for toolName, items := range grouped {
		count := len(items)
		if count == 0 {
			continue
		}
		successCount := 0
		durations := make([]int, 0, count)
		for _, item := range items {
			if item.Success != nil && *item.Success {
				successCount++
			}
			if item.DurationMS != nil {
				durations = append(durations, *item.DurationMS)
			}
		}
		avgDuration := 0.0
		p95Duration := 0.0
		if len(durations) > 0 {
			total := 0
			for _, duration := range durations {
				total += duration
			}
			avgDuration = float64(total) / float64(len(durations))
			p95Duration = float64(percentile95(durations))
		}
		summary = append(summary, toolMonitorSummaryItem{
			ToolName:      toolName,
			Count:         count,
			SuccessRate:   float64(successCount) / float64(count),
			AvgDurationMS: avgDuration,
			P95DurationMS: p95Duration,
		})
	}

	sort.Slice(summary, func(i, j int) bool {
		if summary[i].Count == summary[j].Count {
			return summary[i].ToolName < summary[j].ToolName
		}
		return summary[i].Count > summary[j].Count
	})
	if limit > 0 && len(summary) > limit {
		return summary[:limit]
	}
	return summary
}

func percentile95(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sortedValues := append([]int(nil), values...)
	sort.Ints(sortedValues)
	rank := int(math.Ceil(0.95*float64(len(sortedValues)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sortedValues) {
		rank = len(sortedValues) - 1
	}
	return sortedValues[rank]
}
