// @ts-nocheck
// OMP extension for jp-quality-gate.
// Loaded by Oh My Pi's Bun runtime; no local npm install is required.

const DEFAULT_MAX_RETRIES = 2;
const DEFAULT_DIAGNOSTIC_LIMIT = 20;

type GateIssue = {
  rule?: string;
  severity?: string;
  message?: string;
  text?: string;
  line?: number;
  column?: number;
  details?: Record<string, unknown>;
};

type GateResult = {
  pass?: boolean;
  summary?: { errors?: number; warnings?: number; issues?: number };
  issues?: GateIssue[];
  internal_error?: string;
};

function envInt(name: string, fallback: number): number {
  const raw = process.env[name];
  if (!raw) return fallback;
  const value = Number.parseInt(raw, 10);
  return Number.isFinite(value) ? value : fallback;
}

function truthyEnv(name: string): boolean {
  return /^(1|true|yes|on)$/i.test(process.env[name] ?? "");
}

function extractAssistantText(message: unknown): string {
  if (!message || typeof message !== "object") return "";
  const content = (message as { content?: unknown }).content;

  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";

  return content
    .filter(
      (block): block is { type: "text"; text: string } =>
        !!block &&
        typeof block === "object" &&
        (block as { type?: unknown }).type === "text" &&
        typeof (block as { text?: unknown }).text === "string",
    )
    .map((block) => block.text)
    .join("\n");
}

function gateCommand(): string[] {
  const command = [process.env.JPQG_BIN || "jp-quality-gate"];

  const minCjk = process.env.JPQG_CJ_MIN_CJK;
  if (minCjk) command.push("--cj-min-cjk", minCjk);

  const minGap = process.env.JPQG_CJ_MIN_GAP;
  if (minGap) command.push("--cj-min-gap", minGap);

  if (truthyEnv("JPQG_WARNINGS_AS_ERRORS")) {
    command.push("--warnings-as-errors");
  }

  return command;
}

async function runGate(text: string, signal: AbortSignal) {
  if (signal.aborted) return { aborted: true } as const;

  const command = gateCommand();
  let proc;

  try {
    proc = Bun.spawn(command, {
      stdin: "pipe",
      stdout: "pipe",
      stderr: "pipe",
      env: process.env,
    });
  } catch (error) {
    return {
      aborted: false,
      exitCode: 2,
      stderr: error instanceof Error ? error.message : String(error),
    } as const;
  }

  const abort = () => {
    try {
      proc.kill();
    } catch {
      // Process may already have exited.
    }
  };
  signal.addEventListener("abort", abort, { once: true });

  try {
    proc.stdin.write(text);
    proc.stdin.end();

    const [stdout, stderr, exitCode] = await Promise.all([
      new Response(proc.stdout).text(),
      new Response(proc.stderr).text(),
      proc.exited,
    ]);

    if (signal.aborted) return { aborted: true } as const;

    let result: GateResult | undefined;
    try {
      result = stdout.trim() ? JSON.parse(stdout) : undefined;
    } catch {
      // Invalid JSON is handled as an integration error below.
    }

    return {
      aborted: false,
      exitCode,
      stdout,
      stderr,
      result,
      command,
    } as const;
  } finally {
    signal.removeEventListener("abort", abort);
  }
}

function formatDiagnostics(result: GateResult, limit: number): string {
  const issues = Array.isArray(result.issues) ? result.issues : [];
  const selected = issues.filter((issue) => issue.severity === "error").slice(0, limit);

  if (selected.length === 0) {
    return JSON.stringify(result, null, 2);
  }

  return selected
    .map((issue, index) => {
      const pos =
        typeof issue.line === "number" && typeof issue.column === "number"
          ? ` (${issue.line}:${issue.column})`
          : "";
      const text = issue.text ? `: ${JSON.stringify(issue.text)}` : "";
      const details = issue.details ?? {};
      const candidates = Array.isArray(details.japanese_candidates)
        ? ` -> candidates: ${details.japanese_candidates.join(", ")}`
        : "";
      const message = issue.message ? ` — ${issue.message}` : "";
      return `${index + 1}. ${issue.rule ?? "quality_error"}${pos}${text}${candidates}${message}`;
    })
    .join("\n");
}

function correctionContext(result: GateResult, attempt: number, maxRetries: number): string {
  const limit = Math.max(1, envInt("JPQG_DIAGNOSTIC_LIMIT", DEFAULT_DIAGNOSTIC_LIMIT));
  const diagnostics = formatDiagnostics(result, limit);

  return [
    "[jp-quality-gate] 直前の回答は日本語品質ゲートに失敗しました。",
    `修正試行 ${attempt}/${maxRetries}。`,
    "以下の問題だけを修正し、回答全体を自然な日本語で再出力してください。",
    "技術的な内容、結論、コード、コマンド、パス、識別子、URL、数値は変更しないでください。",
    "中国語の簡体字・繁体字や中国語になっている箇所は、日本語として適切な表記・表現へ直してください。",
    "コードブロックは品質ゲートの対象外なので、必要がなければ変更しないでください。",
    "",
    "検出結果:",
    diagnostics,
  ].join("\n");
}

function reportIntegrationError(ctx: any, message: string) {
  console.error(`[jp-quality-gate] ${message}`);
  try {
    ctx.ui.notify(`jp-quality-gate: ${message}`, "error");
  } catch {
    // Headless/RPC environments may not expose notifications.
  }
}

export default function jpQualityGateExtension(pi: any) {
  const retries = new Map<string, number>();

  pi.on("session_stop", async (event: any, ctx: any) => {
    const text = extractAssistantText(event.last_assistant_message);
    if (!text.trim()) return;

    const key = `${event.session_id}:${event.turn_id}`;
    const maxRetries = Math.max(0, Math.min(7, envInt("JPQG_MAX_RETRIES", DEFAULT_MAX_RETRIES)));

    const gate = await runGate(text, event.signal);
    if (gate.aborted) return;

    if (gate.exitCode === 0 && gate.result?.pass !== false) {
      retries.delete(key);
      return;
    }

    if (gate.exitCode !== 1 || !gate.result) {
      retries.delete(key);
      const stderr = gate.stderr?.trim();
      const detail = gate.result?.internal_error || stderr || `exit code ${gate.exitCode}`;
      reportIntegrationError(ctx, `quality gate could not run (${detail})`);
      return;
    }

    const used = retries.get(key) ?? 0;
    if (used >= maxRetries) {
      retries.delete(key);
      const errors = gate.result.summary?.errors ?? "?";
      reportIntegrationError(
        ctx,
        `still failing after ${maxRetries} correction attempt(s); ${errors} error(s) remain`,
      );
      return;
    }

    const attempt = used + 1;
    retries.set(key, attempt);

    try {
      ctx.ui.notify(
        `jp-quality-gate: 日本語品質エラーを検出。自動修正 ${attempt}/${maxRetries}`,
        "warning",
      );
    } catch {
      // Notification is optional.
    }

    return {
      continue: true,
      additionalContext: correctionContext(gate.result, attempt, maxRetries),
    };
  });
}
