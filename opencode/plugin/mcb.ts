const baseURL = process.env.MCB_URL || "http://127.0.0.1:3411";
const bearerToken = process.env.MCB_BEARER_TOKEN || "";

async function post(path: string, body: unknown) {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (bearerToken) headers.Authorization = `Bearer ${bearerToken}`;
  const response = await fetch(`${baseURL}${path}`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`mcb ${path} failed: ${response.status}`);
  const text = await response.text();
  return text ? JSON.parse(text) : undefined;
}

export default async function mcbPlugin() {
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
  };
}
