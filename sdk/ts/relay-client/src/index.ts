/**
 * relay TypeScript SDK — thin client for the relay daemon.
 *
 * @example
 * ```typescript
 * import { RelayClient } from "@relay/client";
 *
 * const client = new RelayClient("http://localhost:7474");
 * const thread = await client.threadNew("my-run");
 * const { ref } = await client.artifactPut(thread.thread_id, "notes.md", "# Hello", "markdown");
 * const result = await client.capInvoke(thread.thread_id, "retrieval.search", { query: "hello" });
 * console.log(result.preview);
 * ```
 */

export interface Thread {
  thread_id: string;
  name: string;
  state_ref: string;
  created_at: string;
  hop_count?: number;
  artifact_count?: number;
  state_version?: number;
}

export interface StateHeader {
  $schema: string;
  thread_id: string;
  version: number;
  top_facts: Fact[];
  top_constraints: Constraint[];
  open_questions: Question[];
  next_steps: PlanStep[];
  artifact_refs: ArtifactRef[];
  last_actions: Action[];
  metrics: Metrics;
}

export interface Fact {
  id: string;
  key: string;
  value: unknown;
  at?: string;
}

export interface Constraint {
  id: string;
  description: string;
  severity?: "hard" | "soft";
}

export interface Question {
  id: string;
  question: string;
  status: "open" | "resolved";
}

export interface PlanStep {
  id: string;
  step: string;
  status: "pending" | "done" | "skipped";
}

export interface ArtifactRef {
  ref: string;
  type: string;
  name?: string;
}

export interface Action {
  at: string;
  description: string;
  result_ref?: string;
}

export interface Metrics {
  cache_hits: number;
  cache_misses: number;
  tokens_estimate: number;
  tokens_avoided: number;
  hop_count: number;
}

export interface Artifact {
  ref: string;
  thread_id: string;
  type: string;
  mime: string;
  name?: string;
  size: number;
  hash: string;
  preview: Preview;
  provenance: Provenance;
  created_at: string;
}

export interface Preview {
  text?: string;
  line_count?: number;
  truncated: boolean;
  size: number;
}

export interface Provenance {
  created_by: string;
  created_at: string;
  source_refs?: string[];
  capability?: string;
}

export interface PatchOp {
  op: "add" | "remove" | "replace" | "move" | "copy" | "test";
  path: string;
  value?: unknown;
  from?: string;
}

export interface InvokeResult {
  capability: string;
  preview: unknown;
  artifact_ref?: string;
  cache_hit: boolean;
  cache_key?: string;
  duration_ms: number;
}

export interface Capability {
  name: string;
  description: string;
  args_schema: unknown;
  cacheable: boolean;
  cache_ttl_sec?: number;
}

export interface Event {
  id: string;
  thread_id: string;
  type: string;
  payload: unknown;
  timestamp: string;
}

export interface TokenSavings {
  naive_tokens: number;
  actual_tokens: number;
  avoided_tokens: number;
}

export interface ReportResult {
  artifact_ref: string;
  format: string;
  size: number;
  thread_id: string;
  token_savings: TokenSavings;
}

export class RelayError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RelayError";
  }
}

export class RelayAPIError extends RelayError {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(`relay API error ${status}: ${message}`);
    this.name = "RelayAPIError";
  }
}

export interface RelayClientOptions {
  /** Daemon base URL (default: http://localhost:7474) */
  baseUrl?: string;
  /** API token for authentication */
  apiToken?: string;
  /** Request timeout in milliseconds (default: 30000) */
  timeout?: number;
}

export class RelayClient {
  private readonly baseUrl: string;
  private readonly apiToken?: string;
  private readonly timeout: number;

  constructor(options: RelayClientOptions | string = {}) {
    if (typeof options === "string") {
      options = { baseUrl: options };
    }
    this.baseUrl = (options.baseUrl ?? "http://localhost:7474").replace(/\/$/, "");
    this.apiToken = options.apiToken;
    this.timeout = options.timeout ?? 30_000;
  }

  // ---- Threads ----

  async threadNew(name = ""): Promise<Thread> {
    return this.post<Thread>("/threads", { name });
  }

  async threadList(): Promise<Thread[]> {
    const r = await this.get<{ threads: Thread[] }>("/threads");
    return r.threads;
  }

  async threadGet(threadId: string): Promise<Thread> {
    return this.get<Thread>(`/threads/${threadId}`);
  }

  // ---- State ----

  /**
   * Get the bounded state header for use in agent prompts.
   * This is token-efficient: facts/constraints/questions bounded to configured max.
   */
  async stateHeader(threadId: string): Promise<StateHeader> {
    return this.get<StateHeader>(`/threads/${threadId}/state/header`);
  }

  async stateFull(threadId: string): Promise<unknown> {
    return this.get(`/threads/${threadId}/state`);
  }

  /**
   * Apply JSON Patch operations to thread state.
   */
  async statePatch(
    threadId: string,
    ops: PatchOp[],
  ): Promise<{ version: number; state_ref: string; updated_at: string }> {
    return this.post(`/threads/${threadId}/state/patch`, ops);
  }

  // ---- Artifacts ----

  /**
   * Store text content as an artifact.
   * Returns ref — use this instead of pasting content into prompts.
   */
  async artifactPut(
    threadId: string,
    name: string,
    content: string,
    type: string = "text",
    mime: string = "text/plain",
  ): Promise<Artifact> {
    return this.post<Artifact>(`/threads/${threadId}/artifacts`, {
      name,
      type,
      mime,
      content,
    });
  }

  async artifactGet(threadId: string, ref: string): Promise<Artifact> {
    return this.get<Artifact>(`/threads/${threadId}/artifacts/${ref}`);
  }

  async artifactContent(threadId: string, ref: string): Promise<string> {
    return this.getRaw(`/threads/${threadId}/artifacts/${ref}?raw=1`);
  }

  async artifactList(threadId: string): Promise<Artifact[]> {
    const r = await this.get<{ artifacts: Artifact[] }>(`/threads/${threadId}/artifacts`);
    return r.artifacts ?? [];
  }

  // ---- Capabilities ----

  /**
   * Invoke a capability (tool).
   * Returns preview + artifact_ref to avoid re-sending full output.
   */
  async capInvoke(
    threadId: string,
    capability: string,
    args: Record<string, unknown>,
    idempotencyKey?: string,
  ): Promise<InvokeResult> {
    const payload: Record<string, unknown> = { capability, thread_id: threadId, args };
    if (idempotencyKey) payload.idempotency_key = idempotencyKey;
    return this.post<InvokeResult>("/cap/invoke", payload);
  }

  async capList(): Promise<Capability[]> {
    const r = await this.get<{ capabilities: Capability[] }>("/cap/list");
    return r.capabilities ?? [];
  }

  // ---- Reports ----

  async report(threadId: string, format: "md" | "json" = "md"): Promise<ReportResult> {
    return this.post<ReportResult>(`/reports/${threadId}`, { format });
  }

  // ---- Events ----

  async events(threadId: string, after?: string): Promise<Event[]> {
    const path = after
      ? `/threads/${threadId}/events?after=${after}`
      : `/threads/${threadId}/events`;
    const r = await this.get<{ events: Event[] }>(path);
    return r.events ?? [];
  }

  /**
   * Async generator that yields new events as they arrive.
   */
  async *tail(threadId: string, pollInterval = 1000): AsyncGenerator<Event> {
    let lastId: string | undefined;
    while (true) {
      const evs = await this.events(threadId, lastId);
      for (const ev of evs) {
        yield ev;
        lastId = ev.id;
      }
      await sleep(pollInterval);
    }
  }

  // ---- Health ----

  async health(): Promise<{ status: string }> {
    return this.get("/health");
  }

  async version(): Promise<{ version: string; commit: string; built: string }> {
    return this.get("/version");
  }

  // ---- Internals ----

  private headers(): Record<string, string> {
    const h: Record<string, string> = {
      "Content-Type": "application/json",
      Accept: "application/json",
    };
    if (this.apiToken) {
      h["Authorization"] = `Bearer ${this.apiToken}`;
    }
    return h;
  }

  private async get<T>(path: string): Promise<T> {
    return this.request<T>("GET", path);
  }

  private async getRaw(path: string): Promise<string> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeout);
    try {
      const resp = await fetch(this.baseUrl + path, {
        method: "GET",
        headers: this.headers(),
        signal: controller.signal,
      });
      if (!resp.ok) {
        const body = await resp.text();
        throw new RelayAPIError(resp.status, body);
      }
      return resp.text();
    } finally {
      clearTimeout(timer);
    }
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>("POST", path, body);
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeout);

    try {
      const resp = await fetch(this.baseUrl + path, {
        method,
        headers: this.headers(),
        body: body !== undefined ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });

      const text = await resp.text();
      let data: unknown;
      try {
        data = JSON.parse(text);
      } catch {
        data = text;
      }

      if (!resp.ok) {
        const msg =
          typeof data === "object" && data !== null && "error" in data
            ? String((data as { error: string }).error)
            : text;
        throw new RelayAPIError(resp.status, msg);
      }

      return data as T;
    } catch (e) {
      if (e instanceof RelayAPIError || e instanceof RelayError) throw e;
      throw new RelayError(`request failed: ${e}`);
    } finally {
      clearTimeout(timer);
    }
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
