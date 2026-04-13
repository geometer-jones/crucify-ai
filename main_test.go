package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewHandlerServesIndexForKnownEssayRoute(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/essays/the-boundary-error", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "<title>Crucify AI</title>") {
		t.Fatalf("expected essay route to serve the shared index shell")
	}
}

func TestTelemetrySummaryAggregatesEssayMetrics(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	postTelemetry(t, handler, `{"event":"view_start","path":"/essays/the-boundary-error","essayId":"the-boundary-error","viewId":"view-1","visitorId":"visitor-a","sessionId":"session-a","title":"The Boundary Error | Crucify AI","loadTimeMs":1500}`)
	postTelemetry(t, handler, `{"event":"view_end","path":"/essays/the-boundary-error","essayId":"the-boundary-error","viewId":"view-1","visitorId":"visitor-a","sessionId":"session-a","title":"The Boundary Error | Crucify AI","engagedTimeMs":120000,"maxScrollDepthPct":95}`)
	postTelemetry(t, handler, `{"event":"view_start","path":"/essays/the-boundary-error","essayId":"the-boundary-error","viewId":"view-2","visitorId":"visitor-b","sessionId":"session-b","title":"The Boundary Error | Crucify AI","loadTimeMs":900}`)
	postTelemetry(t, handler, `{"event":"view_end","path":"/essays/the-boundary-error","essayId":"the-boundary-error","viewId":"view-2","visitorId":"visitor-b","sessionId":"session-b","title":"The Boundary Error | Crucify AI","engagedTimeMs":30000,"maxScrollDepthPct":40}`)

	req := httptest.NewRequest(http.MethodGet, "/telemetry", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var summary telemetrySummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode telemetry summary: %v", err)
	}

	if summary.Site.Views != 2 {
		t.Fatalf("expected 2 total views, got %d", summary.Site.Views)
	}
	if summary.Site.UniqueVisitors != 2 {
		t.Fatalf("expected 2 unique visitors, got %d", summary.Site.UniqueVisitors)
	}
	if len(summary.Essays) != 1 {
		t.Fatalf("expected 1 essay summary, got %d", len(summary.Essays))
	}

	essay := summary.Essays[0]
	if essay.EssayID != "the-boundary-error" {
		t.Fatalf("expected essay id %q, got %q", "the-boundary-error", essay.EssayID)
	}
	if essay.Path != "/essays/the-boundary-error" {
		t.Fatalf("expected canonical essay path, got %q", essay.Path)
	}
	if essay.Views != 2 {
		t.Fatalf("expected 2 essay views, got %d", essay.Views)
	}
	if essay.UniqueVisitors != 2 {
		t.Fatalf("expected 2 essay visitors, got %d", essay.UniqueVisitors)
	}
	if essay.AvgEngagedTimeMs != 75000 {
		t.Fatalf("expected avg engaged time 75000ms, got %d", essay.AvgEngagedTimeMs)
	}
	if essay.MedianEngagedTimeMs != 75000 {
		t.Fatalf("expected median engaged time 75000ms, got %d", essay.MedianEngagedTimeMs)
	}
	if essay.MaxEngagedTimeMs != 120000 {
		t.Fatalf("expected max engaged time 120000ms, got %d", essay.MaxEngagedTimeMs)
	}
	if essay.AvgScrollDepthPct != 68 {
		t.Fatalf("expected avg scroll depth 68, got %d", essay.AvgScrollDepthPct)
	}
	if essay.AvgLoadTimeMs != 1200 {
		t.Fatalf("expected avg load time 1200ms, got %d", essay.AvgLoadTimeMs)
	}
	if essay.CompletedViews != 1 {
		t.Fatalf("expected 1 completed view, got %d", essay.CompletedViews)
	}
	if essay.CompletionRatePct != 50 {
		t.Fatalf("expected completion rate 50, got %d", essay.CompletionRatePct)
	}
	if essay.Title != "The Boundary Error" {
		t.Fatalf("expected cleaned essay title, got %q", essay.Title)
	}
}

func TestTelemetryRejectsInvalidPayloads(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/telemetry", strings.NewReader(`{"event":"view_start","path":"essays/the-boundary-error"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()

	store, err := newTelemetryStore(filepath.Join(t.TempDir(), "telemetry-events.jsonl"))
	if err != nil {
		t.Fatalf("create telemetry store: %v", err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close telemetry store: %v", err)
		}
	})

	return newHandlerWithStore(store)
}

func postTelemetry(t *testing.T, handler http.Handler, body string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/telemetry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:4242"
	req.Header.Set("User-Agent", "TelemetryTest/1.0")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d for payload %s", http.StatusNoContent, rec.Code, body)
	}
}
