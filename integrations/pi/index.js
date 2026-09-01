import { spawn } from "node:child_process";

const DEFAULT_MAX_RETRIES = 2;
const DEFAULT_DIAGNOSTIC_LIMIT = 20;
const CORRECTION_CUSTOM_TYPE = "jp-quality-gate-correction";

function envInt(name, fallback) {
  const raw = process.env[name];
  if (!raw) return fallback;
  const value = Number.parseInt(raw, 10);
  return Number.isFinite(value) ? value : fallback;
}

function truthyEnv(name) {
  return /^(1|true|yes|on)$/i.test(process.env[name] ?? "");
}

export function extractAssistantText(message) {
  if (!message || typeof message !== "object" || message.role !== "assistant") return "";
  const content = message.content;

  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";

  return content
    .filter(
      (block) =>
        block &&
        typeof block === "object" &&
        block.type === "text" &&
        typeof block.text === "string",
    )
    .map((block) => block.text)
    .join("\n");
}

function hasToolCalls(message) {
  return (
    Array.isArray(message?.content) &&
    message.content.some((block) => block && typeof block === "object" && block.type === "toolCall")
  );
}

export function isTerminalAssistantTurn(event) {
  const message = event?.message;
  if (!message || message.role !== "assistant") return false;
  if (message.stopReason === "error" || message.stopReason === "aborted") return false;
  if (Array.isArray(event.toolResults) && event.toolResults.length > 0) return false;
  if (hasToolCalls(message)) return false;
  return extractAssistantText(message).trim().length > 0;
}

export function filterCorrectionMessages(messages, keepLatest) {
  if (!Array.isArray(messages)) return messages;
  let lastCorrectionIndex = -1;
  if (keepLatest) {
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      const message = messages[index];
      if (message?.role === "custom" && message.customType === CORRECTION_CUSTOM_TYPE) {
        lastCorrectionIndex = index;
        break;
      }
    }
  }

  return messages.filter((message, index) => {
    if (message?.role !== "custom" || message.customType !== CORRECTION_CUSTOM_TYPE) return true;
    return keepLatest && index === lastCorrectionIndex;
  });
}

function gateCommand() {
  const command = process.env.JPQG_BIN || "jp-quality-gate";
  const args = [];

  const minCjk = process.env.JPQG_CJ_MIN_CJK;
  if (minCjk) args.push("--cj-min-cjk", minCjk);

  const minGap = process.env.JPQG_CJ_MIN_GAP;
  if (minGap) args.push("--cj-min-gap", minGap);

  if (truthyEnv("JPQG_WARNINGS_AS_ERRORS")) {
    args.push("--warnings-as-errors");
  }

  return { command, args };
}

async function runGate(text) {
  const { command, args } = gateCommand();

  return await new Promise((resolve) => {
    let settled = false;
    let stdout = "";
    let stderr = "";
    let proc;

    const finish = (result) => {
      if (settled) return;
      settled = true;
      resolve(result);
    };

    try {
      proc = spawn(command, args, {
        env: process.env,
        stdio: ["pipe", "pipe", "pipe"],
      });
    } catch (error) {
      finish({
        exitCode: 2,
        stderr: error instanceof Error ? error.message : String(error),
        command,
        args,
      });
      return;
    }

    proc.stdout.setEncoding("utf8");
    proc.stderr.setEncoding("utf8");
    proc.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    proc.stderr.on("data", (chunk) => {
      stderr += chunk;
    });

    proc.on("error", (error) => {
      finish({
        exitCode: 2,
        stdout,
        stderr: stderr || (error instanceof Error ? error.message : String(error)),
        command,
        args,
      });
    });

    proc.on("close", (code) => {
      let result;
      try {
        result = stdout.trim() ? JSON.parse(stdout) : undefined;
      } catch {
        // Invalid JSON is handled as an integration error by the caller.
      }

      finish({
        exitCode: typeof code === "number" ? code : 2,
        stdout,
        stderr,
        result,
        command,
        args,
      });
    });

    proc.stdin.on("error", (error) => {
      finish({
        exitCode: 2,
        stdout,
        stderr: stderr || (error instanceof Error ? error.message : String(error)),
        command,
        args,
      });
      try {
        proc.kill();
      } catch {
        // Process may already have exited.
      }
    });

    proc.stdin.end(text);
  });
}

function isNonNegativeInteger(value) {
  return Number.isInteger(value) && value >= 0;
}

export function validateGateResult(result) {
  if (!result || typeof result !== "object") return "result is not an object";
  if (typeof result.pass !== "boolean") return "result.pass must be boolean";
  if (!result.summary || typeof result.summary !== "object") return "result.summary is missing";
  if (!Array.isArray(result.issues)) return "result.issues must be an array";

  const { errors, warnings, issues } = result.summary;
  if (!isNonNegativeInteger(errors)) return "summary.errors must be a non-negative integer";
  if (!isNonNegativeInteger(warnings)) return "summary.warnings must be a non-negative integer";
  if (!isNonNegativeInteger(issues)) return "summary.issues must be a non-negative integer";

  let countedErrors = 0;
  let countedWarnings = 0;
  for (const issue of result.issues) {
    if (!issue || typeof issue !== "object") return "every issue must be an object";
    if (typeof issue.rule !== "string" || issue.rule.length === 0) return "every issue must have rule";
    if (issue.severity === "error") countedErrors += 1;
    else if (issue.severity === "warning") countedWarnings += 1;
    else return "issue.severity must be error or warning";
  }

  if (issues !== result.issues.length) return "summary.issues does not match issues.length";
  if (errors !== countedErrors) return "summary.errors does not match issue severities";
  if (warnings !== countedWarnings) return "summary.warnings does not match issue severities";
  if (result.pass !== (countedErrors === 0)) return "result.pass is inconsistent with error count";
  return undefined;
}

function formatDiagnostics(result, limit) {
  const selected = result.issues.filter((issue) => issue.severity === "error").slice(0, limit);

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
      return `${index + 1}. ${issue.rule}${pos}${text}${candidates}${message}`;
    })
    .join("\n");
}

export function correctionContext(result, attempt, maxRetries) {
  const limit = Math.max(1, envInt("JPQG_DIAGNOSTIC_LIMIT", DEFAULT_DIAGNOSTIC_LIMIT));
  const diagnostics = formatDiagnostics(result, limit);

  return [
    "[jp-quality-gate] 直前の回答は日本語品質ゲートに失敗しました。",
    `修正試行 ${attempt}/${maxRetries}。`,
    "以下の問題だけを修正し、回答全体を自然な日本語で再出力してください。",
    "技術的な内容、結論、コード、コマンド、パス、識別子、URL、数値は変更しないでください。",
    "中国語の簡体字・繁体字や中国語になっている箇所は、日本語として適切な表記・表現へ直してください。",
    "コードブロックは品質ゲートの対象外なので、必要がなければ変更しないでください。",
    "新しい作業やツール実行は行わず、直前の回答の日本語だけを修正してください。",
    "",
    "検出結果:",
    diagnostics,
  ].join("\n");
}

export function correctionDeliveryOptions() {
  // turn_end is still inside Pi's active agent run. Steering is consumed before
  // queued follow-ups, so the quality correction cannot be overtaken by unrelated
  // follow-up work that was already waiting.
  return { deliverAs: "steer" };
}

function reportIntegrationError(ctx, message) {
  console.error(`[jp-quality-gate] ${message}`);
  try {
    ctx.ui.notify(`jp-quality-gate: ${message}`, "error");
  } catch {
    // Headless/RPC environments may not expose notifications.
  }
}

export default function jpQualityGatePiExtension(pi) {
  let retries = 0;

  pi.on("agent_start", async () => {
    retries = 0;
  });

  pi.on("context", async (event) => {
    const filtered = filterCorrectionMessages(event.messages, retries > 0);
    return { messages: filtered };
  });

  pi.on("turn_end", async (event, ctx) => {
    if (!isTerminalAssistantTurn(event)) return;

    const text = extractAssistantText(event.message);
    const maxRetries = Math.max(0, Math.min(7, envInt("JPQG_MAX_RETRIES", DEFAULT_MAX_RETRIES)));
    const gate = await runGate(text);

    const schemaError = validateGateResult(gate.result);
    if (gate.exitCode === 0 && !schemaError && gate.result.pass === true) {
      retries = 0;
      return;
    }

    if (gate.exitCode !== 1 || schemaError || gate.result?.pass !== false) {
      const stderr = gate.stderr?.trim();
      const detail =
        gate.result?.internal_error || schemaError || stderr || `exit code ${gate.exitCode}`;
      retries = 0;
      reportIntegrationError(ctx, `quality gate could not run (${detail})`);
      return;
    }

    if (retries >= maxRetries) {
      const errors = gate.result.summary?.errors ?? "?";
      retries = 0;
      reportIntegrationError(
        ctx,
        `still failing after ${maxRetries} correction attempt(s); ${errors} error(s) remain`,
      );
      return;
    }

    retries += 1;
    try {
      ctx.ui.notify(
        `jp-quality-gate: 日本語品質エラーを検出。自動修正 ${retries}/${maxRetries}`,
        "warning",
      );
    } catch {
      // Notification is optional.
    }

    pi.sendMessage(
      {
        customType: CORRECTION_CUSTOM_TYPE,
        content: correctionContext(gate.result, retries, maxRetries),
        display: false,
        details: {
          attempt: retries,
          maxRetries,
          summary: gate.result.summary,
        },
      },
      correctionDeliveryOptions(),
    );
  });
}
