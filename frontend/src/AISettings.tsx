import { useEffect, useRef, useState } from "react";
import { hasNativeBackend, services } from "./services";
import { openWebURL } from "./native";
import type { AILogin, AIPreferences, AIProviderStatus } from "./aiTypes";
import "./AIReview.css";
import { AIModelLogo } from "./AIModelPicker";

const defaults: AIPreferences = {
  provider: "codex",
  model: "",
  intensity: "medium",
  codexPath: "",
  copilotPath: "",
};
export default function AISettings() {
  const native = hasNativeBackend();
  const [preferences, setPreferences] = useState(defaults);
  const [codex, setCodex] = useState<AIProviderStatus>(),
    [copilot, setCopilot] = useState<AIProviderStatus>();
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [notice, setNotice] = useState("");
  const [login, setLogin] = useState<AILogin>();
  const [attempt, setAttempt] = useState(0);
  const guard = useRef(false);
  useEffect(() => {
    if (!native) return;
    let canceled = false;
    setBusy(true);
    setError("");
    void Promise.allSettled([
      services.ai.preferences(),
      services.ai.status("codex"),
      services.ai.status("copilot"),
    ])
      .then((results) => {
        if (canceled) return;
        const [prefs, first, second] = results;
        if (prefs.status === "fulfilled") setPreferences(prefs.value);
        else setError(String(prefs.reason));
        if (first.status === "fulfilled") setCodex(first.value);
        else setError(String(first.reason));
        if (second.status === "fulfilled") setCopilot(second.value);
        else setError(String(second.reason));
      })
      .finally(() => {
        if (!canceled) setBusy(false);
      });
    return () => {
      canceled = true;
    };
  }, [native, attempt]);
  useEffect(() => {
    if (!login) return;
    let canceled = false,
      timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const result = await services.ai.loginStatus(login.loginId);
        if (canceled) return;
        if (result.state === "failed" || result.state === "cancelled") {
          setLogin(undefined);
          setError(result.error || "Sign-in ended. Start a new sign-in.");
          return;
        }
        const status = await services.ai.status("codex");
        if (canceled) return;
        setCodex(status);
        if (status.authenticated) {
          setLogin(undefined);
          setNotice("ChatGPT connected through Codex.");
          return;
        }
        if (result.state === "completed") {
          setLogin(undefined);
          setError(
            status.error ||
              "Sign-in completed, but the ChatGPT account is unavailable. Retry provider detection.",
          );
          return;
        }
      } catch (issue) {
        if (!canceled) {
          setLogin(undefined);
          setError(String(issue));
        }
        return;
      }
      if (!canceled) timer = setTimeout(() => void poll(), 2500);
    };
    timer = setTimeout(() => void poll(), 2500);
    return () => {
      canceled = true;
      clearTimeout(timer);
    };
  }, [login]);
  const run = async (action: () => Promise<void>) => {
    if (guard.current) return;
    guard.current = true;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await action();
    } catch (issue) {
      setError(issue instanceof Error ? issue.message : String(issue));
    } finally {
      guard.current = false;
      setBusy(false);
    }
  };
  return (
    <section className="ai-settings" aria-label="AI providers">
      <h2>AI review</h2>
      <p>
        Use your own ChatGPT or GitHub Copilot subscription to review a pull
        request. Suggestions become private draft comments in M.I.E.R.
      </p>
      <div className="ai-provider-cards">
        {(
          [
            { id: "codex", name: "ChatGPT · Codex", status: codex },
            { id: "copilot", name: "GitHub Copilot", status: copilot },
          ] as const
        ).map((provider) => (
          <article key={provider.id}>
            <header>
              <strong className="ai-provider-brand">
                <AIModelLogo provider={provider.id} />
                {provider.name}
              </strong>
              <span
                className={
                  provider.status?.authenticated ? "text-green" : "muted"
                }
              >
                {provider.status?.authenticated
                  ? "Ready"
                  : provider.status?.installed
                    ? "Sign-in required"
                    : "CLI required"}
              </span>
            </header>
            <p>
              {provider.status?.account}
              {provider.status?.plan && ` · ${provider.status.plan}`}
            </p>
            {provider.status?.error && (
              <p className="form-error">{provider.status.error}</p>
            )}
            {provider.id === "codex" ? (
              <>
                <p>
                  Install Codex and sign in with ChatGPT. M.I.E.R. uses the
                  official local app-server connection.
                </p>
                <code className="ai-install-command">
                  npm install -g @openai/codex
                </code>
                <button
                  className="btn"
                  disabled={!native || busy || !codex?.installed || !!login}
                  onClick={() =>
                    void run(async () => {
                      const result = await services.ai.beginLogin();
                      setLogin(result);
                      await openWebURL(result.url);
                    })
                  }
                >
                  Sign in with ChatGPT
                </button>
              </>
            ) : (
              <>
                <p>
                  Install Copilot, then connect GitHub in Accounts using{" "}
                  <code>gh auth login</code> or a supported OAuth token. Copilot
                  uses that identity and your plan.
                </p>
                <code className="ai-install-command">
                  npm install -g @github/copilot
                </code>
              </>
            )}
          </article>
        ))}
      </div>
      {login && (
        <div className="ai-login" role="status">
          <strong>Complete ChatGPT sign-in in your browser</strong>
          <code>{login.code}</code>
          <button
            className="btn"
            onClick={() =>
              void openWebURL(login.url).catch((issue) =>
                setError(String(issue)),
              )
            }
          >
            Open sign-in page
          </button>
          <button
            className="btn btn-ghost"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                await services.ai.cancelLogin(login.loginId);
                setLogin(undefined);
              })
            }
          >
            Cancel sign-in
          </button>
        </div>
      )}
      <details>
        <summary>Executable paths</summary>
        <p>
          Leave these blank for automatic detection, or enter the full path to
          an installed CLI.
        </p>
        <label>
          Codex executable
          <input
            value={preferences.codexPath}
            disabled={!native || busy}
            placeholder={codex?.executable || "/path/to/codex"}
            onChange={(e) =>
              setPreferences((p) => ({ ...p, codexPath: e.target.value }))
            }
          />
        </label>
        <label>
          Copilot executable
          <input
            value={preferences.copilotPath}
            disabled={!native || busy}
            placeholder={copilot?.executable || "/path/to/copilot"}
            onChange={(e) =>
              setPreferences((p) => ({ ...p, copilotPath: e.target.value }))
            }
          />
        </label>
        <button
          className="btn"
          disabled={!native || busy}
          onClick={() =>
            void run(async () => {
              await services.ai.savePreferences(preferences);
              setNotice("AI preferences saved.");
              setAttempt((n) => n + 1);
            })
          }
        >
          Save AI preferences
        </button>
      </details>
      <div className="ai-settings-actions">
        <button
          className="btn"
          disabled={!native || busy}
          onClick={() => setAttempt((n) => n + 1)}
        >
          {busy ? "Checking providers…" : "Check AI providers"}
        </button>
        <button
          className="btn-ghost"
          onClick={() =>
            void openWebURL(
              "https://github.com/Noah-Wilderom/mier/blob/master/docs/AI.md",
            ).catch((issue) => setError(String(issue)))
          }
        >
          Setup guide
        </button>
      </div>
      {notice && <p role="status">{notice}</p>}
      {error && (
        <p role="alert" className="form-error">
          {error}
        </p>
      )}
      {!native && (
        <p className="prototype-note">
          AI providers are available in the desktop app.
        </p>
      )}
      <h3>MCP tools</h3>
      <p>
        Open a pull request’s AI review pane and choose{" "}
        <strong>Connect an MCP client…</strong> to give your own agent access to
        that review. The connection can read captured source and add draft
        comments. Stop the review to revoke access.
      </p>
    </section>
  );
}
