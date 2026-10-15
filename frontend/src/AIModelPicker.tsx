import { Select } from "radix-ui";
import { Icon } from "./Icon";
import type { AIModel, AIProvider } from "./aiTypes";

function modelBrand(model: string) {
  const value = model.toLowerCase();
  if (/gpt|codex|openai|\bo[134](?:\b|-)/.test(value)) return "openai";
  if (/claude|anthropic/.test(value)) return "claude";
  if (/gemini|google|gemma/.test(value)) return "gemini";
  if (/grok|xai|x-ai/.test(value)) return "grok";
  if (/deepseek/.test(value)) return "deepseek";
  if (/mistral|codestral|devstral/.test(value)) return "mistral";
  if (/qwen/.test(value)) return "qwen";
  if (/llama|meta\//.test(value)) return "meta";
  return undefined;
}

export function AIModelLogo({
  model = "",
  name = "",
  provider,
  size = 18,
}: {
  model?: string;
  name?: string;
  provider?: AIProvider | "external";
  size?: number;
}) {
  const brand =
    model || name
      ? modelBrand(`${model} ${name}`)
      : provider === "codex"
        ? "openai"
        : provider === "copilot"
          ? "githubcopilot"
          : undefined;
  return brand ? (
    <img
      className={`ai-model-logo ai-logo-${brand}`}
      src={`/ai/${brand}.svg`}
      width={size}
      height={size}
      alt=""
      aria-hidden="true"
    />
  ) : (
    <Icon
      name="code"
      size={size}
      className="ai-model-logo"
      aria-hidden="true"
    />
  );
}

export function AIModelPicker({
  models,
  value,
  provider,
  disabled,
  onChange,
}: {
  models: AIModel[];
  value: string;
  provider: AIProvider;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const selected = models.find((model) => model.id === value);
  const options =
    value && !selected
      ? [...models, { id: value, name: value, default: false }]
      : models;
  return (
    <div className="ai-model-picker">
      <label id="ai-model-label">Model</label>
      <Select.Root
        value={value || "provider-default"}
        disabled={disabled}
        onValueChange={(next) =>
          onChange(next === "provider-default" ? "" : next)
        }
      >
        <Select.Trigger
          className="ai-model-trigger"
          aria-label="AI review model"
        >
          <AIModelLogo
            model={value}
            name={selected?.name}
            provider={provider}
          />
          <Select.Value placeholder="Provider default" />
          <Select.Icon>
            <Icon name="chevron-down" size={13} />
          </Select.Icon>
        </Select.Trigger>
        <Select.Portal>
          <Select.Content
            className="ai-model-options"
            position="popper"
            sideOffset={4}
            collisionPadding={8}
          >
            <Select.Viewport>
              <Select.Item value="provider-default" className="ai-model-option">
                <AIModelLogo provider={provider} />
                <Select.ItemText>Provider default</Select.ItemText>
                <Select.ItemIndicator className="ai-model-indicator">
                  <Icon name="check" size={13} />
                </Select.ItemIndicator>
              </Select.Item>
              {options.map((model) => (
                <Select.Item
                  className="ai-model-option"
                  key={model.id}
                  value={model.id}
                >
                  <AIModelLogo model={model.id} name={model.name} />
                  <Select.ItemText>
                    {model.name || model.id}
                    {model.default ? " · default" : ""}
                  </Select.ItemText>
                  <Select.ItemIndicator className="ai-model-indicator">
                    <Icon name="check" size={13} />
                  </Select.ItemIndicator>
                </Select.Item>
              ))}
            </Select.Viewport>
          </Select.Content>
        </Select.Portal>
      </Select.Root>
    </div>
  );
}
