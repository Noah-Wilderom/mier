import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  AssistantRuntimeProvider,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useAuiState,
  useExternalStoreRuntime,
  type TextMessagePartProps,
  type ThreadMessageLike,
} from "@assistant-ui/react";
import { Markdown } from "./Markdown";
import type { AIReview } from "./aiTypes";

const ResultContext = createContext<ReactNode>(null);
const defaultPrompt = "Review this pull request.";
function MessageText({ text }: TextMessagePartProps) {
  return <Markdown>{text}</Markdown>;
}
function UserMessage() {
  return (
    <MessagePrimitive.Root className="ai-chat-message ai-chat-user">
      <strong>You</strong>
      <MessagePrimitive.Parts components={{ Text: MessageText }} />
    </MessagePrimitive.Root>
  );
}
function AssistantMessage() {
  const result = useContext(ResultContext);
  return (
    <MessagePrimitive.Root className="ai-chat-message ai-chat-assistant">
      {result}
    </MessagePrimitive.Root>
  );
}
function DraftObserver({ onChange }: { onChange: (text: string) => void }) {
  const text = useAuiState((state) => state.composer.text);
  useEffect(() => onChange(text), [text, onChange]);
  return null;
}
type Props = {
  run?: AIReview;
  active: boolean;
  busy: boolean;
  disabled: boolean;
  result: ReactNode;
  controls: ReactNode;
  extras: ReactNode;
  onStart: (focus: string) => Promise<boolean>;
  onCancel: () => Promise<void>;
  onDraftChange: (focus: string) => void;
  initialDraft: string;
};
export function AIReviewChat({
  run,
  active,
  busy,
  disabled,
  result,
  controls,
  extras,
  onStart,
  onCancel,
  onDraftChange,
  initialDraft,
}: Props) {
  const [draftAtOpen] = useState(initialDraft);
  const [optionsOpen, setOptionsOpen] = useState(!run);
  useEffect(() => setOptionsOpen(!run), [run?.id]);
  const messages = useMemo<ThreadMessageLike[]>(
    () =>
      run
        ? [
            {
              id: `${run.id}:request`,
              role: "user",
              content: run.instructions || defaultPrompt,
            },
            {
              id: `${run.id}:response`,
              role: "assistant",
              content:
                run.summary || run.progress || run.error || "Review finished.",
              status: active
                ? { type: "running" }
                : { type: "complete", reason: "stop" },
            },
          ]
        : [],
    [run, active],
  );
  const runtime = useExternalStoreRuntime({
    messages,
    convertMessage: (message) => message,
    isRunning: active || busy,
    isSendDisabled: disabled || busy,
    onNew: async (message) => {
      const text = message.content
        .filter((part) => part.type === "text")
        .map((part) => part.text)
        .join("\n")
        .trim();
      const started = await onStart(text === defaultPrompt ? "" : text);
      if (!started)
        runtime.thread.composer.setText(text === defaultPrompt ? "" : text);
    },
    onCancel,
  });
  useEffect(
    () => runtime.thread.composer.setText(draftAtOpen),
    [runtime, draftAtOpen],
  );
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ResultContext.Provider value={result}>
        <ThreadPrimitive.Root
          className="ai-chat-thread"
          aria-label="AI review chat"
        >
          <ThreadPrimitive.Viewport
            className="ai-chat-viewport"
            autoScroll={!!run}
            scrollToBottomOnInitialize={!!run}
          >
            <details
              className="ai-chat-settings"
              open={optionsOpen}
              onToggle={(event) => setOptionsOpen(event.currentTarget.open)}
            >
              <summary>Review options</summary>
              {controls}
            </details>
            {!run && (
              <div className="ai-chat-welcome">
                <strong>A second pair of eyes</strong>
                <p>
                  Review this pull request for concrete bugs, or give the
                  reviewer a focus below.
                </p>
              </div>
            )}
            <ThreadPrimitive.Messages
              components={{ UserMessage, AssistantMessage }}
            />
            <div className="ai-chat-extras">{extras}</div>
          </ThreadPrimitive.Viewport>
          <ComposerPrimitive.Root className="ai-chat-composer">
            <DraftObserver onChange={onDraftChange} />
            <label htmlFor="ai-review-focus">Review focus (optional)</label>
            <ComposerPrimitive.Input
              id="ai-review-focus"
              maxLength={4000}
              rows={2}
              placeholder="Check retries and failure handling…"
              disabled={busy || active}
              addAttachmentOnPaste={false}
              unstable_focusOnRunStart={false}
              unstable_focusOnScrollToBottom={false}
              unstable_focusOnThreadSwitched={false}
            />
            <div className="ai-chat-composer-actions">
              <span className="muted">
                Enter to review · Shift+Enter for a new line
              </span>
              {active ? (
                <ComposerPrimitive.Cancel className="btn" disabled={busy}>
                  Stop review
                </ComposerPrimitive.Cancel>
              ) : (
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={disabled || busy}
                  onClick={() => {
                    if (!runtime.thread.composer.getState().text.trim())
                      runtime.thread.composer.setText(defaultPrompt);
                    runtime.thread.composer.send();
                  }}
                >
                  {busy
                    ? "Preparing review…"
                    : run
                      ? "Run a new review"
                      : "Start AI review"}
                </button>
              )}
            </div>
          </ComposerPrimitive.Root>
        </ThreadPrimitive.Root>
      </ResultContext.Provider>
    </AssistantRuntimeProvider>
  );
}
