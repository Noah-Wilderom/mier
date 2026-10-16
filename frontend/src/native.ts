export type ThemePreference = "system" | "light" | "dark";

export type NativeCommand = {
  action: string;
  value?: string;
};

type NativePlatform = "darwin" | "linux" | "windows";
type NativeWindow = Window & {
  _wails?: {
    environment?: { OS?: string };
    invoke?: (message: string) => void;
  };
};

let bridgeReady = false;

function nativePlatform(): NativePlatform | undefined {
  const os = (window as NativeWindow)._wails?.environment?.OS;
  return os === "darwin" || os === "linux" || os === "windows" ? os : undefined;
}

function syncPlatform() {
  const platform = nativePlatform();
  if (!platform) return;
  document.documentElement.dataset.platform = platform;
  document.documentElement.style.setProperty(
    "--native-titlebar-inset",
    platform === "darwin" ? "80px" : "0px",
  );
  const bridge = (window as NativeWindow)._wails;
  if (!bridgeReady && bridge?.invoke) {
    // Wails queues ExecJS until this handshake, even when no generated bindings are used.
    bridgeReady = true;
    bridge.invoke("wails:runtime:ready");
  }
}

export function subscribeNativeCommands(
  callback: (command: NativeCommand) => void,
): () => void {
  const handleCommand = (event: Event) => {
    const detail: unknown = (event as CustomEvent<unknown>).detail;
    if (
      !detail ||
      typeof detail !== "object" ||
      !("action" in detail) ||
      typeof detail.action !== "string"
    )
      return;
    callback({
      action: detail.action,
      value:
        "value" in detail && typeof detail.value === "string"
          ? detail.value
          : undefined,
    });
  };

  window.addEventListener("mag-ik-een-review:command", handleCommand);
  window.addEventListener("wails:runtime-config-ready", syncPlatform);
  syncPlatform();
  return () => {
    window.removeEventListener("mag-ik-een-review:command", handleCommand);
    window.removeEventListener("wails:runtime-config-ready", syncPlatform);
  };
}

export async function toggleNativeFullscreen(): Promise<boolean> {
  if (!nativePlatform()) return false;
  const { Window: nativeWindow } = await import("@wailsio/runtime");
  await nativeWindow.ToggleFullscreen();
  return true;
}

export async function toggleFullscreen(): Promise<void> {
  if (await toggleNativeFullscreen()) return;
  if (document.fullscreenElement) {
    await document.exitFullscreen();
  } else {
    await document.documentElement.requestFullscreen();
  }
}

export async function openWebURL(value: string): Promise<void> {
  const url = new URL(value);
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) throw new Error('Choose a valid web link.');
  if (nativePlatform()) {
    const {Browser} = await import('@wailsio/runtime');
    await Browser.OpenURL(url.href);
  } else {
    window.open(url.href, '_blank', 'noopener,noreferrer');
  }
}
