import {
  createContext,
  useContext,
  useEffect,
  useId,
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

const ResultContext = createContext<{id?:string;result:ReactNode}>({result:null});
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
  const {id,result} = useContext(ResultContext);
  const messageId=useAuiState(state=>state.message.id);
  return (
    <MessagePrimitive.Root className="ai-chat-message ai-chat-assistant">
      {messageId===`${id}:response`?result:<MessagePrimitive.Parts components={{Text:MessageText}}/>}
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
  history?:AIReview[];
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
  copy?: Partial<{
    prompt: string;
    label: string;
    options: string;
    welcome: string;
    description: string;
    focus: string;
    placeholder: string;
    hint: string;
    stop: string;
    preparing: string;
    start: string;
    again: string;
  }>;
};
export function AIReviewChat({
  run,
  history=[],
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
  copy,
}: Props) {
  const words = {
    prompt: defaultPrompt,
    label: "AI review chat",
    options: "Review options",
    welcome: "A second pair of eyes",
    description:
      "Review this pull request for concrete bugs, or give the reviewer a focus below.",
    focus: "Review focus (optional)",
    placeholder: "Check retries and failure handling…",
    hint: "Enter to send · Shift+Enter for a new line",
    stop: "Stop review",
    preparing: "Preparing review…",
    start: "Start AI review",
    again: "Send follow-up",
    ...copy,
  };
  const [draftAtOpen] = useState(initialDraft);
  const focusId = useId();
  const [optionsOpen, setOptionsOpen] = useState(!run);
  useEffect(() => setOptionsOpen(!run), [run?.id]);
  const messages = useMemo<ThreadMessageLike[]>(()=>{
    const runs=[...history.filter(item=>item.id!==run?.id),...(run?[run]:[])];
    return runs.flatMap(item=>[
      {id:`${item.id}:request`,role:'user' as const,content:item.instructions||words.prompt},
      {id:`${item.id}:response`,role:'assistant' as const,content:[`**${item.provider} · ${item.model||'Provider default'} · ${item.headSha.slice(0,7)}**`,item.summary||item.progress||item.error||'Review finished.',...item.id===run?.id?[]:item.findings.map(finding=>`**${finding.path}:${finding.line}** · ${finding.severity}\n${finding.body}`)].join('\n\n'),status:item.id===run?.id&&active?{type:'running' as const}:{type:'complete' as const,reason:'stop' as const}}
    ]);
  },[history,run,active,words.prompt]);
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
      const started = await onStart(text === words.prompt ? "" : text);
      if (!started)
        runtime.thread.composer.setText(text === words.prompt ? "" : text);
    },
    onCancel,
  });
  useEffect(
    () => runtime.thread.composer.setText(draftAtOpen),
    [runtime, draftAtOpen],
  );
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ResultContext.Provider value={{id:run?.id,result}}>
        <ThreadPrimitive.Root
          className="ai-chat-thread"
          aria-label={words.label}
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
              <summary>{words.options}</summary>
              {controls}
            </details>
            {!run && (
              <div className="ai-chat-welcome">
                <strong>{words.welcome}</strong>
                <p>{words.description}</p>
              </div>
            )}
            <ThreadPrimitive.Messages
              components={{ UserMessage, AssistantMessage }}
            />
            <div className="ai-chat-extras">{extras}</div>
          </ThreadPrimitive.Viewport>
          <ComposerPrimitive.Root className="ai-chat-composer">
            <DraftObserver onChange={onDraftChange} />
            <label htmlFor={focusId}>{words.focus}</label>
            <ComposerPrimitive.Input
              id={focusId}
              maxLength={4000}
              rows={2}
              placeholder={words.placeholder}
              disabled={busy || active}
              addAttachmentOnPaste={false}
              unstable_focusOnRunStart={false}
              unstable_focusOnScrollToBottom={false}
              unstable_focusOnThreadSwitched={false}
            />
            <div className="ai-chat-composer-actions">
              <span className="muted">{words.hint}</span>
              {active ? (
                <ComposerPrimitive.Cancel className="btn" disabled={busy}>
                  {words.stop}
                </ComposerPrimitive.Cancel>
              ) : (
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={disabled || busy}
                  onClick={() => {
                    if (!runtime.thread.composer.getState().text.trim())
                      runtime.thread.composer.setText(words.prompt);
                    runtime.thread.composer.send();
                  }}
                >
                  {busy ? words.preparing : run ? words.again : words.start}
                </button>
              )}
            </div>
          </ComposerPrimitive.Root>
        </ThreadPrimitive.Root>
      </ResultContext.Provider>
    </AssistantRuntimeProvider>
  );
}
