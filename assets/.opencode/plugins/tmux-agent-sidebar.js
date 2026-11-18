// OpenCode V2 plugin bridge for tmux-agent.
//
// OpenCode V2 does not run V1 plugin factories any more: a local plugin must
// default-export a definition object with a stable `id` and an `effect` or
// `setup` function (V2 rejects anything else with
// "Plugin must export a default definition with an id and an effect or setup
// function"). V2 still discovers files in the legacy `plugin/` directory, but
// files there are treated as V1, so `tmux-agent install` writes this file to
// `plugins/` (plural) — the directory V2 documents for V2 plugins.
//
// We deliberately export a plain object instead of calling `Plugin.define()`:
// the plugin runs from `~/.config/opencode`, where the installed
// `@opencode-ai/plugin` package is the V1 SDK and `@opencode/plugin` may not
// resolve at all. The plain object satisfies the same schema with fewer
// moving parts.
//
// V1 -> V2 mapping used here:
//   - `{ directory }` argument        -> `ctx.location.directory`
//   - `event` hook                    -> `ctx.event.subscribe({ signal })` loop
//   - `chat.message` hook             -> `ctx.session.hook("prompt", ...)`
//   - `tool.execute.after` hook       -> `ctx.tool.hook("execute.after", ...)`
//
// The Go side (`internal/adapter/opencode.go` and `tmux-agent hook opencode`)
// keeps the same external event names and JSON payloads as before.

import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const resolveHookScript = () => {
  let dir = dirname(fileURLToPath(import.meta.url));
  for (let i = 0; i < 4; i += 1) {
    const candidate = resolve(dir, "hook.sh");
    if (existsSync(candidate)) {
      return candidate;
    }
    const parent = dirname(dir);
    if (parent === dir) {
      break;
    }
    dir = parent;
  }
  return null;
};

// When the plugin still lives next to the repo (development), call hook.sh
// directly; the installed copy is rewritten by `tmux-agent install opencode`
// to point at the absolute path of the tmux-agent binary instead.
const HOOK_COMMAND = (() => {
  const hookScript = resolveHookScript();
  return hookScript
    ? { cmd: "bash", prefix: [hookScript, "opencode"] }
    : { cmd: "tmux-agent", prefix: ["hook", "opencode"] };
})();

// Fire-and-forget: OpenCode's event stream and hooks must never wait on a
// side-channel report. The child gets the payload on stdin and is never
// awaited, so a missing/slow tmux-agent cannot block the agent loop.
const hook = (eventName, payload) => {
  try {
    const child = spawn(HOOK_COMMAND.cmd, [...HOOK_COMMAND.prefix, eventName], {
      stdio: ["pipe", "ignore", "ignore"],
    });
    child.on("error", () => {});
    child.stdin.on("error", () => {});
    child.stdin.end(JSON.stringify(payload));
  } catch {
    // OpenCode should keep running even if the bridge is missing or
    // the sidebar binary is unavailable.
  }
};

const pickFirstString = (value, keys) => {
  for (const key of keys) {
    const candidate = value?.[key];
    if (typeof candidate === "string" && candidate) {
      return candidate;
    }
  }
  return "";
};

const errorMessage = (err) => {
  if (!err) return "";
  if (typeof err === "string") return err;
  if (typeof err === "object") {
    return pickFirstString(err, ["message", "name"]) || JSON.stringify(err);
  }
  return String(err);
};

// V2 tool results are `{ output?, content?, metadata? }`. V1's sidebar payload
// used `{ title, output, metadata }`, so normalize both shapes back to that:
// `content` (string or typed parts) wins over `output`, and an error becomes
// its message in `output` so the existing activity log keeps working.
const toolOutputText = (result) => {
  if (!result) return "";
  if (typeof result.content === "string") return result.content;
  if (Array.isArray(result.content)) {
    const chunks = [];
    for (const part of result.content) {
      if (typeof part === "string" && part) {
        chunks.push(part);
      } else if (part && typeof part.text === "string" && part.text) {
        chunks.push(part.text);
      } else if (part && typeof part.uri === "string" && part.uri) {
        chunks.push(part.uri);
      }
    }
    if (chunks.length > 0) return chunks.join("\n");
  }
  if (typeof result.output === "string") return result.output;
  if (result.output !== undefined) {
    try {
      return JSON.stringify(result.output);
    } catch {
      return String(result.output);
    }
  }
  return "";
};

const toolResponse = (event) => {
  if (event?.status === "error") {
    return {
      title: "",
      output: errorMessage(event.error),
      metadata: event.error?.metadata ?? null,
    };
  }
  const result = event?.result ?? {};
  return {
    title: "",
    output: toolOutputText(result),
    metadata: result.metadata ?? null,
  };
};

export default {
  id: "tmux-agent",

  async setup(ctx) {
    const cwd =
      typeof ctx?.location?.directory === "string" ? ctx.location.directory : "";

    // V1 `chat.message` is now the session prompt-admission hook. It runs once
    // before admission, carries the prompt text, and exposes a readonly
    // sessionID; synthetic messages never reach it, so the old
    // synthetic/ignored filtering is no longer needed.
    await ctx.session.hook("prompt", (event) => {
      hook("user-prompt-submit", {
        cwd,
        session_id: typeof event?.sessionID === "string" ? event.sessionID : "",
        prompt: typeof event?.prompt?.text === "string" ? event.prompt.text : "",
      });
    });

    // V1 `tool.execute.after` maps to the tool domain hook. V2 passes one
    // event (tool, input, sessionID, plus `status` + `result` or `error`)
    // instead of the old (input, output) pair.
    await ctx.tool.hook("execute.after", (event) => {
      hook("activity-log", {
        cwd,
        session_id: typeof event?.sessionID === "string" ? event.sessionID : "",
        tool_name: typeof event?.tool === "string" ? event.tool : "",
        tool_input: event?.input ?? {},
        tool_response: toolResponse(event),
      });
    });

    // V1 `event` becomes an async subscription to the public server event
    // stream. Abort it from the cleanup function returned by setup.
    const controller = new AbortController();
    void (async () => {
      try {
        for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
          if (!event || !event.type) continue;
          const props = event.properties ?? {};
          const session_id = pickFirstString(props, [
            "sessionID",
            "sessionId",
            "session_id",
          ]);

          switch (event.type) {
            case "session.created":
              hook("session-start", { cwd, session_id, source: "startup" });
              break;

            case "session.status": {
              // Status is a union: { type: "idle" | "busy" | "retry", ... }.
              // `busy` is a secondary status-transition signal — the real
              // prompt text comes from the prompt hook above. The empty-prompt
              // call is a no-op for @pane_prompt (handler guard) but still
              // advances status to "running" when the prompt hook is delayed
              // or missing (e.g. retry -> busy).
              const statusType = props.status?.type;
              if (statusType === "busy") {
                hook("user-prompt-submit", { cwd, session_id, prompt: "" });
              } else if (statusType === "idle") {
                hook("stop", { cwd, session_id, last_message: "" });
              }
              break;
            }

            case "session.idle":
              hook("stop", { cwd, session_id, last_message: "" });
              break;

            case "session.error":
              hook("stop-failure", {
                cwd,
                session_id,
                error: errorMessage(props.error) || "session.error",
              });
              break;

            case "permission.asked":
              hook("notification", { cwd, session_id, wait_reason: "permission" });
              break;
          }
        }
      } catch {
        // The subscription ends by aborting during plugin unload; nothing to
        // report, and the bridge must never crash OpenCode.
      }
    })();

    return () => controller.abort();
  },
};
