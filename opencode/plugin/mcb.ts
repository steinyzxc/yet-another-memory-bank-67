const baseURL = normalizeBaseURL(process.env.MCB_URL || "http://127.0.0.1:3411");
const bearerToken = process.env.MCB_BEARER_TOKEN || "";

const fileTools = new Set(["Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "read", "write", "edit", "glob", "grep"]);
const fileKeys = ["filePath", "file_path", "filepath", "path", "file", "pattern"];
const maxStashedFiles = 20;

let activeSessionID: string | undefined;
let projectPath = process.cwd();
const stashedFiles = new Map<string, Set<string>>();
const contextInjectedSessions = new Set<string>();
const seenToolCalls = new Map<string, Set<string>>();
const seenSubtasks = new Map<string, Set<string>>();
const warnedPostFailures = new Set<string>();

function normalizeBaseURL(value: string) {
  const trimmed = value.trim().replace(/\/+$/, "");
  return trimmed.endsWith("/mcp") ? trimmed.slice(0, -4) : trimmed;
}

async function post(path: string, body: unknown) {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (bearerToken) headers.Authorization = `Bearer ${bearerToken}`;
  const url = `${baseURL}${path}`;
  let response;
  try {
    response = await fetch(url, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
    });
  } catch (error) {
    warnPostFailure(url, `network error: ${safeString(error, 500)}`);
    return undefined;
  }
  if (!response.ok) {
    warnPostFailure(url, `HTTP ${response.status}`);
    return undefined;
  }
  const text = await response.text();
  return text ? JSON.parse(text) : undefined;
}

function warnPostFailure(url: string, message: string) {
  const key = `${url} ${message}`;
  if (warnedPostFailures.has(key)) return;
  warnedPostFailures.add(key);
  console.warn(`[mcb] ${url} failed: ${message}`);
}

async function observe(sessionID: string | undefined, kind: string, payload: Record<string, unknown>, tool = "", cwd = projectPath) {
  if (!sessionID) return;
  await post("/integrations/opencode/event", {
    session_id: sessionID,
    cwd,
    kind,
    tool,
    payload,
  });
}

function stashFor(sessionID: string) {
  let stash = stashedFiles.get(sessionID);
  if (!stash) {
    stash = new Set<string>();
    stashedFiles.set(sessionID, stash);
  }
  return stash;
}

function seenFor(map: Map<string, Set<string>>, sessionID: string) {
  let seen = map.get(sessionID);
  if (!seen) {
    seen = new Set<string>();
    map.set(sessionID, seen);
  }
  return seen;
}

function pruneSession(sessionID: string) {
  stashedFiles.delete(sessionID);
  contextInjectedSessions.delete(sessionID);
  seenToolCalls.delete(sessionID);
  seenSubtasks.delete(sessionID);
}

function safeString(value: unknown, max: number) {
  if (typeof value === "string") return value.slice(0, max);
  if (value == null) return "";
  try {
    return JSON.stringify(value).slice(0, max);
  } catch {
    return String(value).slice(0, max);
  }
}

function extractFiles(value: unknown): string[] {
  const files: string[] = [];
  const visit = (node: unknown) => {
    if (!node || typeof node !== "object") return;
    if (Array.isArray(node)) {
      for (const item of node) visit(item);
      return;
    }
    const record = node as Record<string, unknown>;
    for (const key of Object.keys(record)) {
      const val = record[key];
      if (fileKeys.includes(key) && typeof val === "string" && val.length > 0) files.push(val);
      visit(val);
    }
  };
  visit(value);
  return [...new Set(files)].slice(0, maxStashedFiles);
}

function stashFiles(sessionID: string | undefined, files: string[]) {
  if (!sessionID) return;
  const stash = stashFor(sessionID);
  for (const file of files) stash.add(file);
  if (stash.size > maxStashedFiles) {
    const keep = [...stash].slice(-maxStashedFiles);
    stash.clear();
    for (const file of keep) stash.add(file);
  }
}

function sessionIDFrom(...values: unknown[]) {
  for (const value of values) {
    if (!value || typeof value !== "object") continue;
    const record = value as Record<string, unknown>;
    const candidate = record.sessionID || record.session_id || record.id;
    if (typeof candidate === "string" && candidate.length > 0) return candidate;
  }
  return activeSessionID;
}

const instructions = `<mcb-instructions>
Use mcb for persistent project memory.
- Use mcp__mcb__memory_search or mcp__mcb__memory_recall when past context could help.
- Use mcp__mcb__memory_save when the user asks you to remember a durable fact, preference, decision, or workflow.
- Use mcp__mcb__memory_file_history when it is available before risky file edits.
</mcb-instructions>`;

export default async function mcbPlugin(ctx?: { worktree?: string; project?: { id?: string } }) {
  projectPath = ctx?.worktree || ctx?.project?.id || process.cwd();

  return {
    name: "mcb",

    async context(input: { session_id?: string; cwd?: string }) {
      return post("/integrations/opencode/context", input);
    },

    async tool(input: unknown) {
      await post("/integrations/opencode/tool", input);
    },

    async chat(input: unknown) {
      await post("/integrations/opencode/chat", input);
    },

    async compact(input: unknown) {
      return post("/integrations/opencode/compact", input);
    },

    async event(input: { event?: { type?: string; properties?: Record<string, unknown> } }) {
      const event = input.event;
      if (!event?.type) return;
      const props = event.properties || {};

      if (event.type === "session.created") {
        const info = props.info as Record<string, unknown> | undefined;
        activeSessionID = sessionIDFrom(info, props);
        if (!activeSessionID) return;
        pruneSession(activeSessionID);
        await observe(activeSessionID, "session_created", { title: info?.title ?? null, parent_id: info?.parentID ?? null });
        return;
      }

      const sid = sessionIDFrom(props);
      if (!sid) return;

      switch (event.type) {
        case "session.status": {
          const status = props.status as Record<string, unknown> | undefined;
          await observe(sid, "session_status", {
            status_type: status?.type ?? props.status ?? "unknown",
            attempt: status?.attempt ?? null,
            message: safeString(status?.message, 2000),
          });
          break;
        }
        case "session.compacted":
          await observe(sid, "session_compacted", {});
          await post("/integrations/opencode/compact", { session_id: sid, cwd: projectPath, trigger: "session.compacted" });
          break;
        case "session.updated":
          await observe(sid, "session_updated", { info: props.info ?? null });
          break;
        case "session.diff":
          await observe(sid, "session_diff", { diff: (props.diff as unknown[])?.slice?.(0, 50) ?? [] });
          break;
        case "session.deleted":
          await observe(sid, "session_deleted", {});
          await post("/integrations/opencode/session-end", { session_id: sid, cwd: projectPath });
          if (sid === activeSessionID) activeSessionID = undefined;
          pruneSession(sid);
          break;
        case "session.error":
          await observe(sid, "tool_error", { error: safeString(props.error, 8000) }, "session.error");
          break;
        case "message.updated": {
          const info = props.info as Record<string, unknown> | undefined;
          await observe(sid, info?.role === "assistant" ? "assistant_message" : "message_updated", { info: safeString(info, 8000) });
          break;
        }
        case "message.removed":
          await observe(sid, "message_removed", { message_id: props.messageID ?? null });
          break;
        case "message.part.updated":
          await handlePart(sid, props.part as Record<string, unknown> | undefined);
          break;
        case "file.edited":
          stashFiles(sid, typeof props.file === "string" ? [props.file] : []);
          await observe(sid, "file_edited", { file: props.file ?? null });
          break;
        case "permission.updated":
          await observe(sid, "notification", { notification_type: "permission_prompt", data: safeString(props, 4000) });
          break;
        case "permission.replied":
          await observe(sid, "permission_replied", { response: props.response ?? props.reply ?? null });
          break;
        case "todo.updated":
          await observe(sid, "task_completed", { todos: (props.todos as unknown[])?.slice?.(0, 100) ?? [] });
          break;
        case "command.executed":
          await observe(sid, "command_executed", { name: props.name ?? null, arguments: props.arguments ?? null });
          break;
        default:
          await observe(sid, "opencode_event", { type: event.type, properties: safeString(props, 8000) });
      }
    },

    async "chat.message"(input: Record<string, unknown>, output: Record<string, unknown>) {
      const sid = sessionIDFrom(input);
      if (!sid) return;
      const parts = Array.isArray(output.parts) ? output.parts : [];
      const files = extractFiles(parts);
      stashFiles(sid, files);
      const message = parts
        .filter((part: any) => part?.type === "text" && !part.synthetic && !part.ignored)
        .map((part: any) => part.text || "")
        .join("\n");
      await observe(sid, "user_message", { message: message.slice(0, 8000), files });
    },

    async "chat.params"(input: Record<string, unknown>, output: Record<string, unknown>) {
      const sid = sessionIDFrom(input);
      if (!sid) return;
      await observe(sid, "llm_params", { input: safeString(input, 4000), output: safeString(output, 4000) });
    },

    async "tool.execute.before"(input: Record<string, unknown>, output: Record<string, unknown>) {
      const sid = sessionIDFrom(input);
      const tool = String(input.tool || "");
      if (!sid || !fileTools.has(tool)) return;
      stashFiles(sid, extractFiles(output.args));
      await observe(sid, "pre_tool_use", { tool_input: output.args ?? null }, tool);
    },

    async "experimental.chat.system.transform"(input: Record<string, unknown>, output: Record<string, unknown>) {
      const sid = sessionIDFrom(input);
      if (!sid || !Array.isArray(output.system)) return;
      if (!contextInjectedSessions.has(sid)) {
        output.system.push(instructions);
        const result = await post("/integrations/opencode/context", { session_id: sid, cwd: projectPath });
        const context = result?.additional_context || result?.context;
        if (typeof context === "string" && context.length > 0) output.system.push(context);
        contextInjectedSessions.add(sid);
      }
      const files = [...(stashedFiles.get(sid) || [])].slice(0, 10);
      if (files.length === 0) return;
      const result = await post("/integrations/opencode/enrich", { session_id: sid, cwd: projectPath, files });
      const context = result?.additional_context || result?.context;
      if (typeof context === "string" && context.length > 0) output.system.push(context);
      const stash = stashFor(sid);
      for (const file of files) stash.delete(file);
    },

    async "experimental.session.compacting"(input: Record<string, unknown>, output: Record<string, unknown>) {
      const sid = sessionIDFrom(input);
      if (!sid || !Array.isArray(output.context)) return;
      const result = await post("/integrations/opencode/context", { session_id: sid, cwd: projectPath });
      const context = result?.additional_context || result?.context;
      if (typeof context === "string" && context.length > 0) output.context.push(context);
      const compact = await post("/integrations/opencode/compact", { session_id: sid, cwd: projectPath, trigger: "experimental.session.compacting" });
      const prompt = compact?.prompt;
      if ((compact?.compact || compact?.should_compact) && typeof prompt === "string" && prompt.length > 0) {
        output.context.push(`<mcb-compaction-request>\n${prompt}\n</mcb-compaction-request>`);
      }
    },

    async config(input: Record<string, unknown>) {
      if (!activeSessionID) return;
      await observe(activeSessionID, "config_loaded", {
        model: input.model ?? null,
        agents: Object.keys((input.agent as Record<string, unknown>) || {}),
        mcp_servers: Object.keys((input.mcp as Record<string, unknown>) || {}),
      });
    },
  };
}

async function handlePart(sessionID: string, part?: Record<string, unknown>) {
  if (!part?.type) return;
  if (part.type === "subtask") {
    const id = String(part.id || "");
    const seen = seenFor(seenSubtasks, sessionID);
    if (id && seen.has(id)) return;
    if (id) seen.add(id);
    await observe(sessionID, "subagent_start", { agent: part.agent ?? null, prompt: safeString(part.prompt, 4000) });
    return;
  }
  if (part.type === "tool") {
    const state = part.state as Record<string, unknown> | undefined;
    const callID = String(part.callID || part.id || "");
    const seen = seenFor(seenToolCalls, sessionID);
    if (callID && seen.has(callID)) return;
    const status = state?.status;
    if (status !== "completed" && status !== "error") return;
    if (callID) seen.add(callID);
    const tool = String(part.tool || "");
    if (fileTools.has(tool)) stashFiles(sessionID, extractFiles(state?.input));
    await observe(sessionID, status === "error" ? "tool_error" : "tool_use", {
      call_id: callID || null,
      tool_input: safeString(state?.input, 4000),
      tool_response: safeString(status === "error" ? state?.error : state?.output, 8000),
    }, tool);
    return;
  }
  if (part.type === "file") stashFiles(sessionID, extractFiles(part));
  const kind = String(part.type).replace(/[^a-zA-Z0-9_]+/g, "_").toLowerCase();
  await observe(sessionID, kind, { part: safeString(part, 8000) });
}
