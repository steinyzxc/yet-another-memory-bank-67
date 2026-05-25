package admin

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/steinyzxc/yet-another-memory-bank-67/internal/secrets"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type importJSONLStats struct {
	Files    int
	Imported int
	Skipped  int
}

func runImportJSONL(ctx context.Context, args []string, io IO) int {
	opts, rest, err := parseOptions(args, io)
	if err != nil {
		fmt.Fprintf(io.Stderr, "%v\n", err)
		return 2
	}
	if len(rest) == 0 {
		fmt.Fprintln(io.Stderr, "missing transcript path")
		return 2
	}
	s, err := store.Open(ctx, opts.dbPath)
	if err != nil {
		fmt.Fprintf(io.Stderr, "open store: %v\n", err)
		return 1
	}
	defer s.Close()

	var total importJSONLStats
	for _, root := range rest {
		paths, err := collectJSONLPaths(root)
		if err != nil {
			fmt.Fprintf(io.Stderr, "collect transcripts: %v\n", err)
			return 1
		}
		for _, path := range paths {
			stats, err := importClaudeJSONLFile(ctx, s, path, opts.project, io.Now())
			if err != nil {
				fmt.Fprintf(io.Stderr, "import %s: %v\n", path, err)
				return 1
			}
			total.Files += stats.Files
			total.Imported += stats.Imported
			total.Skipped += stats.Skipped
		}
	}
	fmt.Fprintf(io.Stdout, "files=%d imported=%d skipped=%d\n", total.Files, total.Imported, total.Skipped)
	return 0
}

func collectJSONLPaths(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{root}, nil
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".jsonl") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func importClaudeJSONLFile(ctx context.Context, s *store.Store, path, defaultProject string, importedAt int64) (importJSONLStats, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return importJSONLStats{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return importJSONLStats{}, err
	}
	defer f.Close()

	stats := importJSONLStats{Files: 1}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := append([]byte(nil), bytes.TrimSpace(scanner.Bytes())...)
		if len(raw) == 0 {
			continue
		}
		obs, eventID, err := parseClaudeTranscriptEvent(raw, absPath, lineNo, defaultProject, importedAt)
		if err != nil {
			return importJSONLStats{}, fmt.Errorf("line %d: %w", lineNo, err)
		}
		exists, err := s.ImportedEventExists(ctx, absPath, eventID)
		if err != nil {
			return importJSONLStats{}, err
		}
		if exists {
			stats.Skipped++
			continue
		}
		inserted, err := s.InsertObservation(ctx, obs, 0)
		if err != nil {
			return importJSONLStats{}, err
		}
		recorded, err := s.RecordImportedEvent(ctx, absPath, eventID, importedAt)
		if err != nil {
			return importJSONLStats{}, err
		}
		if inserted && recorded {
			stats.Imported++
		} else {
			stats.Skipped++
		}
	}
	if err := scanner.Err(); err != nil {
		return importJSONLStats{}, err
	}
	return stats, nil
}

func parseClaudeTranscriptEvent(raw []byte, transcriptPath string, lineNo int, defaultProject string, fallbackTS int64) (store.ObservationInput, string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return store.ObservationInput{}, "", err
	}
	eventID := firstJSONLString(obj, "uuid", "id", "event_id", "eventId")
	if eventID == "" {
		eventID = fmt.Sprintf("line:%d", lineNo)
	}
	sessionID := firstJSONLString(obj, "sessionId", "session_id")
	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(transcriptPath), filepath.Ext(transcriptPath))
	}
	cwd := firstJSONLString(obj, "cwd", "project")
	if cwd == "" {
		cwd = defaultProject
	}
	ts := firstJSONLTimestamp(obj, fallbackTS, "timestamp", "created_at", "createdAt")
	tool := firstJSONLString(obj, "tool_name", "toolName", "tool", "name")
	if tool == "" {
		tool = inferClaudeToolName(obj)
	}
	return store.ObservationInput{
		Agent:             "claude-code",
		ExternalSessionID: sessionID,
		CWD:               cwd,
		TS:                ts,
		Kind:              claudeTranscriptKind(obj),
		Tool:              tool,
		PayloadJSON:       []byte(secrets.Redact(string(raw))),
		Hash:              importEventHash(transcriptPath, eventID),
	}, eventID, nil
}

func firstJSONLString(obj map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if raw, ok := obj[key]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				return s
			}
			var n json.Number
			if err := json.Unmarshal(raw, &n); err == nil {
				return n.String()
			}
		}
	}
	return ""
}

func firstJSONLTimestamp(obj map[string]json.RawMessage, fallback int64, keys ...string) int64 {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if ts, ok := parseJSONLTimestampString(s); ok {
				return ts
			}
		}
		var n int64
		if err := json.Unmarshal(raw, &n); err == nil {
			return normalizeJSONLUnix(n)
		}
		var f float64
		if err := json.Unmarshal(raw, &f); err == nil {
			return normalizeJSONLUnix(int64(f))
		}
	}
	return fallback
}

func parseJSONLTimestampString(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		return normalizeJSONLUnix(ts), true
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UnixMilli(), true
	}
	return 0, false
}

func normalizeJSONLUnix(value int64) int64 {
	if value > 0 && value < 1_000_000_000_000 {
		return value * 1000
	}
	return value
}

func claudeTranscriptKind(obj map[string]json.RawMessage) string {
	typ := strings.ToLower(firstJSONLString(obj, "type", "event_type", "eventType"))
	typ = strings.ReplaceAll(typ, "-", "_")
	switch {
	case typ == "user":
		return "user_message"
	case typ == "assistant":
		return "assistant_message"
	case typ == "summary":
		return "summary"
	case typ == "tool_response" || typ == "tool_result":
		return "tool_result"
	case typ == "tool_use" || typ == "tool":
		return "tool_use"
	case strings.Contains(typ, "tool") && strings.Contains(typ, "error"):
		return "tool_error"
	case strings.Contains(typ, "subagent") && strings.Contains(typ, "start"):
		return "subagent_start"
	case strings.Contains(typ, "subagent") && strings.Contains(typ, "stop"):
		return "subagent_stop"
	case typ != "":
		return typ
	default:
		return "transcript_event"
	}
}

func inferClaudeToolName(obj map[string]json.RawMessage) string {
	messageRaw, ok := obj["message"]
	if !ok {
		return ""
	}
	var message struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(messageRaw, &message); err != nil || len(message.Content) == 0 {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(message.Content, &blocks); err != nil {
		return ""
	}
	for _, block := range blocks {
		if block.Type == "tool_use" && block.Name != "" {
			return block.Name
		}
	}
	return ""
}

func importEventHash(transcriptPath, eventID string) string {
	sum := sha256.Sum256([]byte(transcriptPath + "\x00" + eventID))
	return "import:" + hex.EncodeToString(sum[:])
}
