package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhase2TimelineFileHistoryExportAuditAndVerify(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	_, err = s.EnsureSession(ctx, "claude-code", "s1", "/repo", 1000)
	if err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	_, err = s.InsertObservation(ctx, ObservationInput{Agent: "claude-code", ExternalSessionID: "s1", CWD: "/repo", TS: 1100, Kind: "tool_use", Tool: "Read", PayloadJSON: []byte(`{"file_path":"a.go"}`), Hash: "h1"}, 300)
	if err != nil {
		t.Fatalf("insert observation: %v", err)
	}
	memoryID, err := s.AddMemory(ctx, MemoryInput{Project: "/repo", Text: "a.go uses table-driven tests", SessionID: "claude-code:s1", CreatedAt: 1200})
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	text := "a.go uses subtests"
	if err := s.UpdateMemory(ctx, MemoryUpdate{ID: memoryID, Text: &text, UpdatedAt: 1300}); err != nil {
		t.Fatalf("update memory: %v", err)
	}

	timeline, err := s.Timeline(ctx, TimelineFilter{Project: "/repo", Limit: 10})
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(timeline) != 2 || timeline[0].Type != "observation" || timeline[1].Type != "memory" {
		t.Fatalf("timeline = %+v", timeline)
	}

	history, err := s.FileHistory(ctx, FileHistoryFilter{Project: "/repo", Files: []string{"a.go"}, Limit: 10})
	if err != nil {
		t.Fatalf("file history: %v", err)
	}
	if len(history.Memories) != 1 || len(history.Observations) != 1 {
		t.Fatalf("history = %+v", history)
	}

	export, err := s.Export(ctx, ExportFilter{Project: "/repo", Limit: 10})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(export.Memories) != 1 || len(export.Sessions) != 1 || len(export.Observations) != 1 {
		t.Fatalf("export = %+v", export)
	}

	audit, err := s.AuditEvents(ctx, AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(audit) < 2 || audit[0].Action == "" {
		t.Fatalf("audit = %+v", audit)
	}

	verified, err := s.VerifyMemory(ctx, memoryID)
	if err != nil {
		t.Fatalf("verify memory: %v", err)
	}
	if verified.Memory.ID != memoryID || len(verified.Observations) != 1 || !strings.Contains(string(verified.Observations[0].PayloadJSON), "a.go") {
		t.Fatalf("verified = %+v", verified)
	}
}

func TestPatternsAggregatesRepeatedCaptureData(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	_, _ = s.InsertObservation(ctx, ObservationInput{Agent: "opencode", ExternalSessionID: "o1", CWD: "/repo", TS: 1000, Kind: "tool_error", Tool: "Read", PayloadJSON: []byte(`{"file":"a.go","error":"not found"}`), Hash: "p1"}, 300)
	_, _ = s.InsertObservation(ctx, ObservationInput{Agent: "opencode", ExternalSessionID: "o1", CWD: "/repo", TS: 1100, Kind: "tool_error", Tool: "Read", PayloadJSON: []byte(`{"file":"a.go","error":"not found"}`), Hash: "p2"}, 300)

	patterns, err := s.Patterns(ctx, PatternFilter{Project: "/repo", Limit: 10})
	if err != nil {
		t.Fatalf("patterns: %v", err)
	}
	if patterns.TopTools["Read"] != 2 || patterns.TopKinds["tool_error"] != 2 || patterns.Files["a.go"] != 2 {
		t.Fatalf("patterns = %+v", patterns)
	}
}
