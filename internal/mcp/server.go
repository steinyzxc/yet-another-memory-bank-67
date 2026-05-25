package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	mcbsearch "github.com/steinyzxc/yet-another-memory-bank-67/internal/search"
	"github.com/steinyzxc/yet-another-memory-bank-67/internal/store"
)

type Options struct {
	DefaultProject string
	SearchConfig   mcbsearch.Config
	Embedder       mcbsearch.Embedder
	CircuitBreaker mcbsearch.CircuitBreaker
	Now            func() int64
}

type Server struct {
	store *store.Store
	opts  Options
}

func New(s *store.Store, opts Options) http.Handler {
	return &Server{store: s, opts: opts}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		writeRPC(w, nil, nil, &rpcError{Code: -32700, Message: "invalid json-rpc request"})
		return
	}

	switch req.Method {
	case "initialize":
		writeRPC(w, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{"name": "mcb", "version": "dev"},
		}, nil)
	case "notifications/initialized":
		writeRPC(w, req.ID, map[string]any{}, nil)
	case "tools/list":
		writeRPC(w, req.ID, map[string]any{"tools": tools()}, nil)
	case "resources/list":
		writeRPC(w, req.ID, map[string]any{"resources": resources()}, nil)
	case "resources/read":
		result, err := s.readResource(r.Context(), req.Params)
		if err != nil {
			writeRPC(w, req.ID, nil, &rpcError{Code: -32602, Message: err.Error()})
			return
		}
		writeRPC(w, req.ID, result, nil)
	case "tools/call":
		result, err := s.callTool(r.Context(), req.Params)
		if err != nil {
			writeRPC(w, req.ID, nil, &rpcError{Code: -32602, Message: err.Error()})
			return
		}
		writeRPC(w, req.ID, result, nil)
	default:
		writeRPC(w, req.ID, nil, &rpcError{Code: -32601, Message: "method not found"})
	}
}

func writeRPC(w http.ResponseWriter, id json.RawMessage, result any, rpcErr *rpcError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]any{"jsonrpc": "2.0"}
	if len(id) > 0 {
		resp["id"] = id
	} else {
		resp["id"] = nil
	}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	_ = json.NewEncoder(w).Encode(resp)
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil || params.Name == "" {
		return nil, fmt.Errorf("invalid tool call")
	}
	if len(params.Arguments) == 0 || string(params.Arguments) == "null" {
		params.Arguments = []byte(`{}`)
	}
	var payload any
	var err error
	switch params.Name {
	case "memory_recall":
		payload, err = s.memoryRecall(ctx, params.Arguments, true)
	case "memory_search":
		payload, err = s.memoryRecall(ctx, params.Arguments, false)
	case "memory_save":
		payload, err = s.memorySave(ctx, params.Arguments)
	case "memory_sessions":
		payload, err = s.memorySessions(ctx, params.Arguments)
	case "memory_session_observations":
		payload, err = s.memorySessionObservations(ctx, params.Arguments)
	case "memory_forget":
		payload, err = s.memoryForget(ctx, params.Arguments)
	case "memory_profile":
		payload, err = s.memoryProfile(ctx, params.Arguments)
	case "memory_session_summary_save":
		payload, err = s.memorySessionSummarySave(ctx, params.Arguments)
	case "memory_supersede":
		payload, err = s.memorySupersede(ctx, params.Arguments)
	case "memory_update":
		payload, err = s.memoryUpdate(ctx, params.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool %q", params.Name)
	}
	if err != nil {
		return nil, err
	}
	return toolText(payload)
}

func (s *Server) readResource(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var in struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.URI == "" {
		return nil, fmt.Errorf("resource uri is required")
	}
	var payload any
	var err error
	switch {
	case in.URI == "mcb://status":
		payload, err = s.store.Status(ctx)
	case strings.HasPrefix(in.URI, "mcb://project/") && strings.HasSuffix(in.URI, "/profile"):
		project := strings.TrimSuffix(strings.TrimPrefix(in.URI, "mcb://project/"), "/profile")
		payload, err = s.profilePayload(ctx, project)
	default:
		return nil, fmt.Errorf("unknown resource %q", in.URI)
	}
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal resource: %w", err)
	}
	return map[string]any{"contents": []map[string]string{{"uri": in.URI, "mimeType": "application/json", "text": string(data)}}}, nil
}

func toolText(payload any) (map[string]any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal tool result: %w", err)
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(data)}}}, nil
}

func (s *Server) memoryRecall(ctx context.Context, raw json.RawMessage, touch bool) (any, error) {
	var in struct {
		Query   string `json:"query"`
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || strings.TrimSpace(in.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	project := s.project(in.Project)
	limit := normalizedLimit(in.Limit, 10, 100)
	results, err := s.search(ctx, in.Query, project, limit, touch)
	if err != nil {
		return nil, err
	}
	return map[string]any{"memories": memoryDTOs(results)}, nil
}

func (s *Server) search(ctx context.Context, query, project string, limit int, touch bool) ([]store.Memory, error) {
	if s.opts.Embedder != nil {
		cfg := s.opts.SearchConfig
		cfg.SkipTouch = !touch
		if cfg.Now == nil {
			cfg.Now = s.now
		}
		results, err := (mcbsearch.Searcher{Store: s.store, Embedder: s.opts.Embedder, CircuitBreaker: s.opts.CircuitBreaker, Config: cfg}).Hybrid(ctx, mcbsearch.Query{Text: query, Project: project, Limit: limit})
		if err == nil {
			memories := make([]store.Memory, 0, len(results))
			for _, result := range results {
				memories = append(memories, result.Memory)
			}
			return memories, nil
		}
	}
	memories, err := s.store.SearchMemories(ctx, store.MemorySearch{Project: project, Query: query, Limit: limit})
	if err != nil {
		return nil, err
	}
	if touch {
		ids := make([]int64, 0, len(memories))
		for _, memory := range memories {
			ids = append(ids, memory.ID)
		}
		if err := s.store.TouchMemories(ctx, ids, s.now()); err != nil {
			return nil, err
		}
	}
	return memories, nil
}

func (s *Server) memorySave(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Text       string  `json:"text"`
		Tier       string  `json:"tier"`
		Project    string  `json:"project"`
		Importance float64 `json:"importance"`
		SessionID  string  `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	project := s.project(in.Project)
	if in.Tier == "" {
		in.Tier = "semantic"
	}
	if in.Importance < 0 || in.Importance > 1 {
		return nil, fmt.Errorf("importance must be between 0 and 1")
	}
	if in.Importance == 0 && in.SessionID != "" {
		in.Importance = 0.5
	}
	if in.SessionID != "" {
		if err := s.ensureNormalizedSession(ctx, in.SessionID, project); err != nil {
			return nil, err
		}
	}
	id, err := s.store.AddMemory(ctx, store.MemoryInput{Project: project, Text: in.Text, Tier: in.Tier, Source: "mcp", Importance: in.Importance, SessionID: in.SessionID, CreatedAt: s.now()})
	if err != nil {
		return nil, fmt.Errorf("save memory: %w", err)
	}
	if in.SessionID != "" {
		_ = s.store.MarkCompactionCompleted(ctx, in.SessionID, s.now())
	}
	return map[string]any{"id": id}, nil
}

func (s *Server) ensureNormalizedSession(ctx context.Context, id, project string) error {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("session_id must be normalized as agent:external_id")
	}
	_, err := s.store.EnsureSession(ctx, parts[0], parts[1], project, s.now())
	if err != nil {
		return fmt.Errorf("ensure session: %w", err)
	}
	return nil
}

func (s *Server) memorySessions(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Project string `json:"project"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("invalid sessions arguments")
	}
	sessions, err := s.store.ListSessions(ctx, s.project(in.Project), normalizedLimit(in.Limit, 10, 100))
	if err != nil {
		return nil, err
	}
	return map[string]any{"sessions": sessionDTOs(sessions)}, nil
}

func (s *Server) memorySessionObservations(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	observations, err := s.store.ListSessionObservations(ctx, in.SessionID, normalizedLimit(in.Limit, 100, 1000))
	if err != nil {
		return nil, err
	}
	return map[string]any{"observations": observationDTOs(observations)}, nil
}

func (s *Server) memoryForget(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Query   string  `json:"query"`
		Project string  `json:"project"`
		DryRun  bool    `json:"dry_run"`
		IDs     []int64 `json:"ids"`
		Confirm bool    `json:"confirm"`
		Limit   int     `json:"limit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("invalid forget arguments")
	}
	if strings.TrimSpace(in.Query) != "" {
		if !in.DryRun {
			return nil, fmt.Errorf("query forget requires dry_run=true")
		}
		memories, err := s.search(ctx, in.Query, s.project(in.Project), normalizedLimit(in.Limit, 10, 100), false)
		if err != nil {
			return nil, err
		}
		return map[string]any{"candidates": memoryDTOs(memories), "dry_run": true}, nil
	}
	if len(in.IDs) == 0 {
		return nil, fmt.Errorf("ids are required for delete")
	}
	if !in.Confirm {
		return nil, fmt.Errorf("confirm=true is required for delete")
	}
	deleted, err := s.store.DeleteMemories(ctx, in.IDs)
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": deleted}, nil
}

func (s *Server) memoryProfile(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		Project string `json:"project"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("invalid profile arguments")
	}
	return s.profilePayload(ctx, in.Project)
}

func (s *Server) profilePayload(ctx context.Context, project string) (any, error) {
	if project == "" {
		profiles, err := s.store.ProjectProfiles(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(profiles))
		for _, profile := range profiles {
			items = append(items, projectProfileDTO(profile))
		}
		return map[string]any{"projects": items}, nil
	}
	profile, err := s.store.ProjectProfile(ctx, s.project(project))
	if err != nil {
		return nil, err
	}
	return projectProfileDTO(profile), nil
}

func projectProfileDTO(profile store.ProjectProfile) map[string]any {
	return map[string]any{
		"project":           profile.Project,
		"memory_count":      profile.MemoryCount,
		"session_count":     profile.SessionCount,
		"observation_count": profile.ObservationCount,
		"top_tiers":         profile.TopTiers,
		"top_tools":         profile.TopTools,
		"files_touched":     profile.FilesTouched,
	}
}

func (s *Server) memorySessionSummarySave(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		SessionID string `json:"session_id"`
		Summary   string `json:"summary"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.SessionID == "" || strings.TrimSpace(in.Summary) == "" {
		return nil, fmt.Errorf("session_id and summary are required")
	}
	if len(in.Summary) > 800 {
		return nil, fmt.Errorf("summary must be <= 800 characters")
	}
	now := s.now()
	if err := s.store.SaveSessionSummary(ctx, in.SessionID, in.Summary, now); err != nil {
		return nil, err
	}
	if err := s.store.MarkCompactionCompleted(ctx, in.SessionID, now); err != nil {
		return nil, err
	}
	return map[string]any{"updated": true}, nil
}

func (s *Server) memorySupersede(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		OldID int64 `json:"old_id"`
		NewID int64 `json:"new_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.OldID <= 0 || in.NewID <= 0 {
		return nil, fmt.Errorf("old_id and new_id are required")
	}
	if err := s.store.SupersedeMemory(ctx, in.OldID, in.NewID); err != nil {
		return nil, err
	}
	return map[string]any{"updated": true}, nil
}

func (s *Server) memoryUpdate(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		ID         int64    `json:"id"`
		Text       *string  `json:"text"`
		Tier       *string  `json:"tier"`
		Importance *float64 `json:"importance"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.ID <= 0 {
		return nil, fmt.Errorf("id is required")
	}
	if in.Text != nil && strings.TrimSpace(*in.Text) == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}
	if in.Importance != nil && (*in.Importance < 0 || *in.Importance > 1) {
		return nil, fmt.Errorf("importance must be between 0 and 1")
	}
	if err := s.store.UpdateMemory(ctx, store.MemoryUpdate{ID: in.ID, Text: in.Text, Tier: in.Tier, Importance: in.Importance, UpdatedAt: s.now()}); err != nil {
		return nil, err
	}
	return map[string]any{"updated": true}, nil
}

func (s *Server) project(value string) string {
	if value != "" {
		return value
	}
	return s.opts.DefaultProject
}

func (s *Server) now() int64 {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now().UnixMilli()
}

func normalizedLimit(value, def, max int) int {
	if value <= 0 {
		value = def
	}
	if value > max {
		value = max
	}
	return value
}

func memoryDTOs(memories []store.Memory) []map[string]any {
	results := make([]map[string]any, 0, len(memories))
	for _, memory := range memories {
		results = append(results, map[string]any{
			"id":          memory.ID,
			"project":     memory.Project,
			"text":        memory.Text,
			"tier":        memory.Tier,
			"source":      memory.Source,
			"importance":  memory.Importance,
			"session_id":  memory.SessionID,
			"created_at":  memory.CreatedAt,
			"updated_at":  memory.UpdatedAt,
			"accessed_at": memory.AccessedAt,
			"score":       memory.Score,
		})
	}
	return results
}

func sessionDTOs(sessions []store.Session) []map[string]any {
	results := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		results = append(results, map[string]any{
			"id":          session.ID,
			"agent":       session.Agent,
			"external_id": session.ExternalID,
			"project":     session.Project,
			"started_at":  session.StartedAt,
			"ended_at":    session.EndedAt,
			"summary":     session.Summary,
			"n_obs":       session.NObs,
		})
	}
	return results
}

func observationDTOs(observations []store.Observation) []map[string]any {
	results := make([]map[string]any, 0, len(observations))
	for _, observation := range observations {
		var payload any
		if err := json.Unmarshal(observation.PayloadJSON, &payload); err != nil {
			payload = string(observation.PayloadJSON)
		}
		results = append(results, map[string]any{
			"id":         observation.ID,
			"session_id": observation.SessionID,
			"cwd":        observation.CWD,
			"ts":         observation.TS,
			"kind":       observation.Kind,
			"tool":       observation.Tool,
			"payload":    payload,
		})
	}
	return results
}

func tools() []map[string]any {
	return []map[string]any{
		tool("memory_recall", "Search project memories and refresh access metadata for returned memories.", map[string]any{"query": schemaString(true), "project": schemaString(false), "limit": schemaInteger(false)}),
		tool("memory_save", "Save a durable memory fact for the current project.", map[string]any{"text": schemaString(true), "tier": schemaString(false), "project": schemaString(false), "importance": schemaNumber(false), "session_id": schemaString(false)}),
		tool("memory_search", "Search project memories without refreshing access metadata.", map[string]any{"query": schemaString(true), "project": schemaString(false), "limit": schemaInteger(false)}),
		tool("memory_sessions", "List captured sessions for a project.", map[string]any{"project": schemaString(false), "limit": schemaInteger(false)}),
		tool("memory_session_observations", "List decoded observations captured for a session.", map[string]any{"session_id": schemaString(true), "limit": schemaInteger(false)}),
		tool("memory_forget", "Dry-run by query or delete explicitly confirmed memory IDs.", map[string]any{"query": schemaString(false), "project": schemaString(false), "dry_run": schemaBoolean(false), "ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "confirm": schemaBoolean(false), "limit": schemaInteger(false)}),
		tool("memory_profile", "Return aggregate memory and capture statistics for a project.", map[string]any{"project": schemaString(false)}),
		tool("memory_session_summary_save", "Save a concise compaction summary for a captured session.", map[string]any{"session_id": schemaString(true), "summary": schemaString(true)}),
		tool("memory_supersede", "Mark an old memory as superseded by a newer memory.", map[string]any{"old_id": schemaInteger(true), "new_id": schemaInteger(true)}),
		tool("memory_update", "Edit an existing memory's text, tier, or importance.", map[string]any{"id": schemaInteger(true), "text": schemaString(false), "tier": schemaString(false), "importance": schemaNumber(false)}),
	}
}

func resources() []map[string]any {
	return []map[string]any{
		{"uri": "mcb://status", "name": "mcb status", "mimeType": "application/json", "description": "Counts and status for the memory bank."},
		{"uri": "mcb://project/{project}/profile", "name": "project profile", "mimeType": "application/json", "description": "Aggregate memory and capture statistics for one project."},
	}
}

func tool(name, description string, properties map[string]any) map[string]any {
	required := []string{}
	for name, schema := range properties {
		if m, ok := schema.(map[string]any); ok {
			if req, _ := m["x-required"].(bool); req {
				required = append(required, name)
				delete(m, "x-required")
			}
		}
	}
	return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required}}
}

func schemaString(required bool) map[string]any  { return schema("string", required) }
func schemaInteger(required bool) map[string]any { return schema("integer", required) }
func schemaNumber(required bool) map[string]any  { return schema("number", required) }
func schemaBoolean(required bool) map[string]any { return schema("boolean", required) }

func schema(typ string, required bool) map[string]any {
	return map[string]any{"type": typ, "x-required": required}
}
