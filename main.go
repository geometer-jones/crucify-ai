package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	siteTitle             = "Crucify AI"
	maxTelemetryBodyBytes = 16 << 10
)

var essayRouteAliases = map[string]string{
	"on-consciousness": "self-architecture",
}

type telemetryEvent struct {
	Event             string `json:"event"`
	Path              string `json:"path"`
	EssayID           string `json:"essayId,omitempty"`
	ViewID            string `json:"viewId,omitempty"`
	VisitorID         string `json:"visitorId,omitempty"`
	SessionID         string `json:"sessionId,omitempty"`
	Title             string `json:"title,omitempty"`
	Referrer          string `json:"referrer,omitempty"`
	Language          string `json:"language,omitempty"`
	Timezone          string `json:"timezone,omitempty"`
	VisibilityState   string `json:"visibilityState,omitempty"`
	EndReason         string `json:"endReason,omitempty"`
	ViewportWidth     int    `json:"viewportWidth,omitempty"`
	ViewportHeight    int    `json:"viewportHeight,omitempty"`
	ScreenWidth       int    `json:"screenWidth,omitempty"`
	ScreenHeight      int    `json:"screenHeight,omitempty"`
	LoadTimeMs        int64  `json:"loadTimeMs,omitempty"`
	EngagedTimeMs     int64  `json:"engagedTimeMs,omitempty"`
	MaxScrollDepthPct int    `json:"maxScrollDepthPct,omitempty"`
}

type telemetryLogRecord struct {
	ReceivedAt time.Time      `json:"receivedAt"`
	ClientIP   string         `json:"clientIP,omitempty"`
	UserAgent  string         `json:"userAgent,omitempty"`
	Event      telemetryEvent `json:"event"`
}

type telemetryView struct {
	ViewID            string
	VisitorKey        string
	SessionID         string
	Path              string
	CanonicalPath     string
	EssayID           string
	Title             string
	Referrer          string
	Language          string
	Timezone          string
	ClientIP          string
	UserAgent         string
	ViewportWidth     int
	ViewportHeight    int
	ScreenWidth       int
	ScreenHeight      int
	LoadTimeMs        int64
	EngagedTimeMs     int64
	MaxScrollDepthPct int
	StartedAt         time.Time
	LastSeenAt        time.Time
	EndedAt           time.Time
	LastVisibility    string
	LastEndReason     string
}

type telemetryTotals struct {
	Views               int   `json:"views"`
	UniqueVisitors      int   `json:"uniqueVisitors"`
	UniqueSessions      int   `json:"uniqueSessions"`
	AvgEngagedTimeMs    int64 `json:"avgEngagedTimeMs"`
	MedianEngagedTimeMs int64 `json:"medianEngagedTimeMs"`
	MaxEngagedTimeMs    int64 `json:"maxEngagedTimeMs"`
	AvgScrollDepthPct   int   `json:"avgScrollDepthPct"`
	AvgLoadTimeMs       int64 `json:"avgLoadTimeMs"`
}

type telemetryPageSummary struct {
	Path                string `json:"path"`
	EssayID             string `json:"essayId,omitempty"`
	Title               string `json:"title"`
	Views               int    `json:"views"`
	UniqueVisitors      int    `json:"uniqueVisitors"`
	UniqueSessions      int    `json:"uniqueSessions"`
	AvgEngagedTimeMs    int64  `json:"avgEngagedTimeMs"`
	MedianEngagedTimeMs int64  `json:"medianEngagedTimeMs"`
	MaxEngagedTimeMs    int64  `json:"maxEngagedTimeMs"`
	AvgScrollDepthPct   int    `json:"avgScrollDepthPct"`
	AvgLoadTimeMs       int64  `json:"avgLoadTimeMs"`
	CompletedViews      int    `json:"completedViews"`
	CompletionRatePct   int    `json:"completionRatePct"`
	LastViewedAt        string `json:"lastViewedAt,omitempty"`
}

type telemetrySummary struct {
	GeneratedAt string                 `json:"generatedAt"`
	Site        telemetryTotals        `json:"site"`
	Pages       []telemetryPageSummary `json:"pages"`
	Essays      []telemetryPageSummary `json:"essays"`
}

type telemetryAccumulator struct {
	Path         string
	EssayID      string
	Title        string
	Views        int
	VisitorKeys  map[string]struct{}
	SessionIDs   map[string]struct{}
	EngagedTimes []int64
	ScrollDepths []int
	LoadTimes    []int64
	MaxEngagedMs int64
	Completed    int
	LastViewedAt time.Time
}

type telemetryStore struct {
	mu    sync.RWMutex
	path  string
	file  *os.File
	views map[string]*telemetryView
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4173"
	}

	handler, err := newHandler()
	if err != nil {
		log.Fatalf("initialize handler: %v", err)
	}

	log.Printf("Serving crucify-ai on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}

func newHandler() (http.Handler, error) {
	store, err := newTelemetryStore(telemetryDataPath())
	if err != nil {
		return nil, err
	}

	return newHandlerWithStore(store), nil
}

func newHandlerWithStore(store *telemetryStore) http.Handler {
	essayIDs := loadEssayIDs("essays")
	fs := http.FileServer(http.Dir("."))
	mux := http.NewServeMux()

	mux.HandleFunc("/telemetry", handleTelemetry(store))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if essayID, ok := essayIDFromPath(r.URL.Path); ok {
			if _, exists := essayIDs[essayID]; exists {
				http.ServeFile(w, r, "index.html")
				return
			}
		}

		fs.ServeHTTP(w, r)
	})

	return mux
}

func telemetryDataPath() string {
	if path := strings.TrimSpace(os.Getenv("TELEMETRY_DATA_PATH")); path != "" {
		return path
	}

	return filepath.Join("data", "telemetry-events.jsonl")
}

func newTelemetryStore(path string) (*telemetryStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("missing telemetry data path")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	store := &telemetryStore{
		path:  path,
		file:  file,
		views: make(map[string]*telemetryView),
	}

	if err := store.load(); err != nil {
		file.Close()
		return nil, err
	}

	return store, nil
}

func (s *telemetryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return nil
	}

	err := s.file.Close()
	s.file = nil
	return err
}

func (s *telemetryStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return err
	}

	scanner := bufio.NewScanner(s.file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record telemetryLogRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return err
		}

		record.Event = normalizeTelemetryEvent(record.Event)
		s.applyRecordLocked(record)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	_, err := s.file.Seek(0, io.SeekEnd)
	return err
}

func (s *telemetryStore) Record(record telemetryLogRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}

	if _, err := s.file.Write(append(payload, '\n')); err != nil {
		return err
	}

	s.applyRecordLocked(record)
	return nil
}

func (s *telemetryStore) Summary() telemetrySummary {
	s.mu.RLock()
	views := make([]telemetryView, 0, len(s.views))
	for _, view := range s.views {
		views = append(views, *view)
	}
	s.mu.RUnlock()

	pageAccumulators := make(map[string]*telemetryAccumulator)
	essayAccumulators := make(map[string]*telemetryAccumulator)
	siteVisitors := make(map[string]struct{})
	siteSessions := make(map[string]struct{})
	siteEngaged := make([]int64, 0, len(views))
	siteScrolls := make([]int, 0, len(views))
	siteLoads := make([]int64, 0, len(views))
	var siteMaxEngaged int64

	for _, view := range views {
		if view.Path == "" {
			continue
		}

		if view.VisitorKey != "" {
			siteVisitors[view.VisitorKey] = struct{}{}
		}
		if view.SessionID != "" {
			siteSessions[view.SessionID] = struct{}{}
		}

		siteEngaged = append(siteEngaged, view.EngagedTimeMs)
		siteScrolls = append(siteScrolls, view.MaxScrollDepthPct)
		if view.LoadTimeMs > 0 {
			siteLoads = append(siteLoads, view.LoadTimeMs)
		}
		if view.EngagedTimeMs > siteMaxEngaged {
			siteMaxEngaged = view.EngagedTimeMs
		}

		pageKey := telemetryPageKey(view)
		pageAccumulators[pageKey] = accumulateTelemetry(pageAccumulators[pageKey], view)
		if view.EssayID != "" {
			essayAccumulators[view.EssayID] = accumulateTelemetry(essayAccumulators[view.EssayID], view)
		}
	}

	pages := summarizeAccumulators(pageAccumulators)
	essays := summarizeAccumulators(essayAccumulators)

	return telemetrySummary{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Site: telemetryTotals{
			Views:               len(views),
			UniqueVisitors:      len(siteVisitors),
			UniqueSessions:      len(siteSessions),
			AvgEngagedTimeMs:    averageInt64(siteEngaged),
			MedianEngagedTimeMs: medianInt64(siteEngaged),
			MaxEngagedTimeMs:    siteMaxEngaged,
			AvgScrollDepthPct:   averageInt(siteScrolls),
			AvgLoadTimeMs:       averageInt64(siteLoads),
		},
		Pages:  pages,
		Essays: essays,
	}
}

func (s *telemetryStore) applyRecordLocked(record telemetryLogRecord) {
	if record.Event.ViewID == "" {
		return
	}

	view := s.views[record.Event.ViewID]
	if view == nil {
		view = &telemetryView{
			ViewID: record.Event.ViewID,
		}
		s.views[record.Event.ViewID] = view
	}

	event := record.Event
	if view.Path == "" && event.Path != "" {
		view.Path = event.Path
	}
	if event.EssayID != "" {
		view.EssayID = event.EssayID
		view.CanonicalPath = canonicalEssayPath(event.EssayID)
	} else if view.CanonicalPath == "" && event.Path != "" {
		view.CanonicalPath = event.Path
	}
	if title := cleanPageTitle(event.Title); title != "" {
		view.Title = title
	}
	if event.Referrer != "" {
		view.Referrer = event.Referrer
	}
	if event.Language != "" {
		view.Language = event.Language
	}
	if event.Timezone != "" {
		view.Timezone = event.Timezone
	}
	if event.VisibilityState != "" {
		view.LastVisibility = event.VisibilityState
	}
	if event.EndReason != "" {
		view.LastEndReason = event.EndReason
	}
	if event.ViewportWidth > 0 {
		view.ViewportWidth = event.ViewportWidth
	}
	if event.ViewportHeight > 0 {
		view.ViewportHeight = event.ViewportHeight
	}
	if event.ScreenWidth > 0 {
		view.ScreenWidth = event.ScreenWidth
	}
	if event.ScreenHeight > 0 {
		view.ScreenHeight = event.ScreenHeight
	}
	if event.LoadTimeMs > 0 && (view.LoadTimeMs == 0 || event.LoadTimeMs < view.LoadTimeMs) {
		view.LoadTimeMs = event.LoadTimeMs
	}
	if event.EngagedTimeMs > view.EngagedTimeMs {
		view.EngagedTimeMs = event.EngagedTimeMs
	}
	if event.MaxScrollDepthPct > view.MaxScrollDepthPct {
		view.MaxScrollDepthPct = event.MaxScrollDepthPct
	}

	if visitorKey := buildVisitorKey(event.VisitorID, record.ClientIP, record.UserAgent); visitorKey != "" {
		view.VisitorKey = visitorKey
	}
	if event.SessionID != "" {
		view.SessionID = event.SessionID
	}
	if record.ClientIP != "" {
		view.ClientIP = record.ClientIP
	}
	if record.UserAgent != "" {
		view.UserAgent = record.UserAgent
	}

	if view.StartedAt.IsZero() || record.ReceivedAt.Before(view.StartedAt) {
		view.StartedAt = record.ReceivedAt
	}
	if record.ReceivedAt.After(view.LastSeenAt) {
		view.LastSeenAt = record.ReceivedAt
	}
	if event.Event == "view_end" && (view.EndedAt.IsZero() || record.ReceivedAt.After(view.EndedAt)) {
		view.EndedAt = record.ReceivedAt
	}
}

func handleTelemetry(store *telemetryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")

		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, store.Summary())
			return
		case http.MethodPost:
			handleTelemetryEvent(store, w, r)
			return
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}
}

func handleTelemetryEvent(store *telemetryStore, w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxTelemetryBodyBytes))
	if err != nil {
		http.Error(w, "failed to read telemetry payload", http.StatusBadRequest)
		return
	}

	var event telemetryEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid telemetry payload", http.StatusBadRequest)
		return
	}

	event = normalizeTelemetryEvent(event)
	if err := validateTelemetryEvent(event); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	record := telemetryLogRecord{
		ReceivedAt: time.Now().UTC(),
		ClientIP:   clientIPFromRequest(r),
		UserAgent:  strings.TrimSpace(r.UserAgent()),
		Event:      event,
	}

	if err := store.Record(record); err != nil {
		http.Error(w, "failed to record telemetry", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write json response: %v", err)
	}
}

func normalizeTelemetryEvent(event telemetryEvent) telemetryEvent {
	event.Event = strings.TrimSpace(event.Event)
	event.Path = normalizeTelemetryPath(event.Path)
	event.EssayID = canonicalEssayID(strings.TrimSpace(event.EssayID))
	if event.EssayID == "" {
		if essayID, ok := essayIDFromPath(event.Path); ok {
			event.EssayID = canonicalEssayID(essayID)
		}
	}
	event.ViewID = strings.TrimSpace(event.ViewID)
	event.VisitorID = strings.TrimSpace(event.VisitorID)
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.Title = strings.TrimSpace(event.Title)
	event.Referrer = strings.TrimSpace(event.Referrer)
	event.Language = strings.TrimSpace(event.Language)
	event.Timezone = strings.TrimSpace(event.Timezone)
	event.VisibilityState = strings.TrimSpace(event.VisibilityState)
	event.EndReason = strings.TrimSpace(event.EndReason)
	if event.MaxScrollDepthPct < 0 {
		event.MaxScrollDepthPct = 0
	}
	if event.MaxScrollDepthPct > 100 {
		event.MaxScrollDepthPct = 100
	}
	if event.LoadTimeMs < 0 {
		event.LoadTimeMs = 0
	}
	if event.EngagedTimeMs < 0 {
		event.EngagedTimeMs = 0
	}
	return event
}

func normalizeTelemetryPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "/" {
		return path
	}

	return strings.TrimRight(path, "/")
}

func validateTelemetryEvent(event telemetryEvent) error {
	switch event.Event {
	case "view_start", "view_ping", "view_end":
	default:
		return errTelemetry("unknown event type")
	}

	switch {
	case event.Path == "":
		return errTelemetry("missing path")
	case !strings.HasPrefix(event.Path, "/"):
		return errTelemetry("path must start with '/'")
	case len(event.Path) > 2048:
		return errTelemetry("path too long")
	case event.ViewID == "":
		return errTelemetry("missing view id")
	case len(event.ViewID) > 128:
		return errTelemetry("view id too long")
	case len(event.VisitorID) > 128:
		return errTelemetry("visitor id too long")
	case len(event.SessionID) > 128:
		return errTelemetry("session id too long")
	case len(event.EssayID) > 128:
		return errTelemetry("essay id too long")
	case len(event.Title) > 512:
		return errTelemetry("title too long")
	case len(event.Referrer) > 2048:
		return errTelemetry("referrer too long")
	case event.MaxScrollDepthPct < 0 || event.MaxScrollDepthPct > 100:
		return errTelemetry("scroll depth must be between 0 and 100")
	}

	return nil
}

type errTelemetry string

func (e errTelemetry) Error() string {
	return string(e)
}

func buildVisitorKey(visitorID, clientIP, userAgent string) string {
	if visitorID = strings.TrimSpace(visitorID); visitorID != "" {
		return "visitor:" + visitorID
	}

	if clientIP == "" && userAgent == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(strings.TrimSpace(clientIP) + "\x00" + strings.TrimSpace(userAgent)))
	return "fingerprint:" + hex.EncodeToString(sum[:8])
}

func telemetryPageKey(view telemetryView) string {
	if view.EssayID != "" {
		return "essay:" + view.EssayID
	}

	return "path:" + view.Path
}

func accumulateTelemetry(acc *telemetryAccumulator, view telemetryView) *telemetryAccumulator {
	if acc == nil {
		acc = &telemetryAccumulator{
			Path:        view.Path,
			EssayID:     view.EssayID,
			Title:       view.Title,
			VisitorKeys: make(map[string]struct{}),
			SessionIDs:  make(map[string]struct{}),
		}
		if view.EssayID != "" {
			acc.Path = canonicalEssayPath(view.EssayID)
		}
	}

	acc.Views++
	if view.Title != "" {
		acc.Title = view.Title
	}
	if view.VisitorKey != "" {
		acc.VisitorKeys[view.VisitorKey] = struct{}{}
	}
	if view.SessionID != "" {
		acc.SessionIDs[view.SessionID] = struct{}{}
	}
	acc.EngagedTimes = append(acc.EngagedTimes, view.EngagedTimeMs)
	acc.ScrollDepths = append(acc.ScrollDepths, view.MaxScrollDepthPct)
	if view.LoadTimeMs > 0 {
		acc.LoadTimes = append(acc.LoadTimes, view.LoadTimeMs)
	}
	if view.EngagedTimeMs > acc.MaxEngagedMs {
		acc.MaxEngagedMs = view.EngagedTimeMs
	}
	if view.MaxScrollDepthPct >= 90 {
		acc.Completed++
	}
	if view.LastSeenAt.After(acc.LastViewedAt) {
		acc.LastViewedAt = view.LastSeenAt
	}

	return acc
}

func summarizeAccumulators(accumulators map[string]*telemetryAccumulator) []telemetryPageSummary {
	summaries := make([]telemetryPageSummary, 0, len(accumulators))
	for _, acc := range accumulators {
		summary := telemetryPageSummary{
			Path:                acc.Path,
			EssayID:             acc.EssayID,
			Title:               acc.Title,
			Views:               acc.Views,
			UniqueVisitors:      len(acc.VisitorKeys),
			UniqueSessions:      len(acc.SessionIDs),
			AvgEngagedTimeMs:    averageInt64(acc.EngagedTimes),
			MedianEngagedTimeMs: medianInt64(acc.EngagedTimes),
			MaxEngagedTimeMs:    acc.MaxEngagedMs,
			AvgScrollDepthPct:   averageInt(acc.ScrollDepths),
			AvgLoadTimeMs:       averageInt64(acc.LoadTimes),
			CompletedViews:      acc.Completed,
			CompletionRatePct:   percentage(acc.Completed, acc.Views),
		}
		if summary.Title == "" {
			summary.Title = fallbackSummaryTitle(acc.Path, acc.EssayID)
		}
		if !acc.LastViewedAt.IsZero() {
			summary.LastViewedAt = acc.LastViewedAt.UTC().Format(time.RFC3339Nano)
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Views != summaries[j].Views {
			return summaries[i].Views > summaries[j].Views
		}
		if summaries[i].Path != summaries[j].Path {
			return summaries[i].Path < summaries[j].Path
		}
		return summaries[i].Title < summaries[j].Title
	})

	return summaries
}

func fallbackSummaryTitle(path, essayID string) string {
	if essayID != "" {
		return strings.ReplaceAll(essayID, "-", " ")
	}
	if path == "/" {
		return siteTitle
	}
	return path
}

func percentage(part, total int) int {
	if total == 0 {
		return 0
	}

	return int(math.Round((float64(part) / float64(total)) * 100))
}

func averageInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}

	var sum int64
	for _, value := range values {
		sum += value
	}

	return int64(math.Round(float64(sum) / float64(len(values))))
}

func averageInt(values []int) int {
	if len(values) == 0 {
		return 0
	}

	sum := 0
	for _, value := range values {
		sum += value
	}

	return int(math.Round(float64(sum) / float64(len(values))))
}

func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}

	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}

	return int64(math.Round(float64(sorted[middle-1]+sorted[middle]) / 2))
}

func cleanPageTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}

	suffix := " | " + siteTitle
	if strings.HasSuffix(title, suffix) {
		return strings.TrimSpace(strings.TrimSuffix(title, suffix))
	}

	return title
}

func canonicalEssayID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}

	if alias, ok := essayRouteAliases[id]; ok {
		return alias
	}

	return id
}

func canonicalEssayPath(essayID string) string {
	if essayID == "" {
		return ""
	}

	return "/essays/" + essayID
}

func clientIPFromRequest(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}

	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}

	return strings.TrimSpace(r.RemoteAddr)
}

func loadEssayIDs(dir string) map[string]struct{} {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("load essay ids: %v", err)
	}

	ids := make(map[string]struct{}, len(entries)+len(essayRouteAliases))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".html" {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".html")
		if id == "" {
			continue
		}

		ids[id] = struct{}{}
	}

	for alias := range essayRouteAliases {
		ids[alias] = struct{}{}
	}

	return ids
}

func essayIDFromPath(path string) (string, bool) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "", false
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 || parts[0] != "essays" || parts[1] == "" {
		return "", false
	}

	return parts[1], true
}
