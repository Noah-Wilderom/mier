import type { ReactNode, SVGProps } from "react";

type IconProps = Omit<SVGProps<SVGSVGElement>, "children"> & {
  name: string;
  size?: number;
};

const icons: Record<string, ReactNode> = {
  bell: <><path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9"/><path d="M10 21h4"/></>,
  branch: (
    <>
      <circle cx="6" cy="5" r="2" />
      <circle cx="6" cy="19" r="2" />
      <circle cx="18" cy="5" r="2" />
      <path d="M6 7v10m12-10v2a6 6 0 0 1-6 6H6" />
    </>
  ),
  inbox: (
    <>
      <path d="M4 4h16l2 10v6H2v-6L4 4Z" />
      <path d="M2 14h6l2 3h4l2-3h6" />
    </>
  ),
  "pull-request": (
    <>
      <circle cx="6" cy="5" r="2" />
      <circle cx="6" cy="19" r="2" />
      <circle cx="18" cy="19" r="2" />
      <path d="M6 7v10m12 0V9a4 4 0 0 0-4-4h-2m3-3-3 3 3 3" />
    </>
  ),
  history: (
    <>
      <path d="M3 11a9 9 0 1 1 2.6 7M3 5v6h6" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  files: (
    <>
      <path d="M8 3h8l4 4v13H8V3Zm8 0v4h4M4 7v15h12" />
    </>
  ),
  file: (
    <>
      <path d="M6 3h8l4 4v14H6V3Zm8 0v4h4" />
    </>
  ),
  folder: <path d="M3 5h6l2 2h10v13H3V5Z" />,
  "folder-open": (
    <>
      <path d="M3 10V5h6l2 2h9v3" />
      <path d="M5 10h17l-3 10H2l3-10Z" />
    </>
  ),
  "chevron-down": <path d="m6 9 6 6 6-6" />,
  "chevron-right": <path d="m9 6 6 6-6 6" />,
  "chevron-left": <path d="m15 6-6 6 6 6" />,
  "chevron-up": <path d="m6 15 6-6 6 6" />,
  plus: <path d="M12 5v14M5 12h14" />,
  edit: <><path d="m16 3 5 5-12 12-6 1 1-6L16 3Z"/><path d="m14 5 5 5"/></>,
  search: (
    <>
      <circle cx="10.5" cy="10.5" r="6.5" />
      <path d="m16 16 5 5" />
    </>
  ),
  settings: (
    <>
      <path d="m10 3-1 3-3 1-2-1-2 4 2 2v2l-2 2 2 4 3-1 2 1 1 3h4l1-3 2-1 3 1 2-4-2-2v-2l2-2-2-4-3 1-3-1-1-3h-3Z" />
      <circle cx="12" cy="13" r="3" />
    </>
  ),
  check: <path d="m5 12 4 4L19 6" />,
  "check-circle": (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="m7.5 12 3 3 6-6" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  "alert-circle": (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v6m0 4h.01" />
    </>
  ),
  close: <path d="m6 6 12 12M6 18 18 6" />,
  "arrow-right": <path d="M4 12h16m-6-6 6 6-6 6" />,
  "arrow-left": <path d="M20 12H4m6-6-6 6 6 6" />,
  "arrow-up": <path d="M12 20V4m-6 6 6-6 6 6" />,
  "arrow-down": <path d="M12 4v16m-6-6 6 6 6-6" />,
  more: (
    <>
      <circle cx="5" cy="12" r="1" />
      <circle cx="12" cy="12" r="1" />
      <circle cx="19" cy="12" r="1" />
    </>
  ),
  expand: <path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5" />,
  collapse: <path d="M3 8h5V3m13 5h-5V3M8 21v-5H3m13 5v-5h5" />,
  columns: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M12 4v16" />
    </>
  ),
  code: <path d="m8 6-6 6 6 6m8-12 6 6-6 6m-3-14-2 16" />,
  comment: <path d="M21 4H3v14h4v4l5-4h9V4Z" />,
  "message-circle": (
    <path d="M21 11.5A8.5 8.5 0 0 1 12.5 20a9 9 0 0 1-4-1L3 21l2-5.5a9 9 0 0 1-1-4A8.5 8.5 0 0 1 12.5 3 8.5 8.5 0 0 1 21 11.5Z" />
  ),
  reply: <path d="m9 5-6 6 6 6M3 11h9a8 8 0 0 1 8 8" />,
  "align-left": <path d="M3 5h18M3 10h12M3 15h18M3 20h12" />,
  "file-code": (
    <>
      <path d="M6 3h8l4 4v14H6V3Zm8 0v4h4m-7 5-2 2 2 2m3-4 2 2-2 2" />
    </>
  ),
  "git-commit": (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M2 12h6m8 0h6" />
    </>
  ),
  "git-merge": (
    <>
      <circle cx="6" cy="5" r="2" />
      <circle cx="6" cy="19" r="2" />
      <circle cx="18" cy="7" r="2" />
      <path d="M6 7v10m12-8a8 8 0 0 1-8 8H6" />
    </>
  ),
  refresh: (
    <>
      <path d="M20 8a9 9 0 0 0-15-3L3 8m0-5v5h5M4 16a9 9 0 0 0 15 3l2-3m0 5v-5h-5" />
    </>
  ),
  moon: <path d="M20.8 13.3A9 9 0 0 1 10.7 3.2a9 9 0 1 0 10.1 10.1Z" />,
  sun: (
    <>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" />
    </>
  ),
  monitor: (
    <>
      <rect x="3" y="3" width="18" height="13" rx="2" />
      <path d="M12 16v5m-5 0h10" />
    </>
  ),
  command: (
    <path d="M8 8H5.5a2.5 2.5 0 1 1 2.5-2.5V18.5a2.5 2.5 0 1 1-2.5-2.5h13a2.5 2.5 0 1 1-2.5 2.5v-13a2.5 2.5 0 1 1 2.5 2.5H8Z" />
  ),
  github: (
    <>
      <path d="M8 21v-4c-4 1-5-2-5-2m13 6v-4a4 4 0 0 0-1-3c3-.4 6-1.5 6-6a5 5 0 0 0-1.5-3.5A5 5 0 0 0 19.3 1s-1.3-.4-4.3 1.6a15 15 0 0 0-6 0C6 .6 4.7 1 4.7 1a5 5 0 0 0-.2 3.5A5 5 0 0 0 3 8c0 4.5 3 5.6 6 6a4 4 0 0 0-1 3" />
    </>
  ),
  external: (
    <>
      <path d="M14 3h7v7m0-7L11 13M10 3H3v18h18v-7" />
    </>
  ),
  filter: <path d="M3 4h18l-7 8v7l-4 2v-9L3 4Z" />,
  eye: (
    <>
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z" />
      <circle cx="12" cy="12" r="3" />
    </>
  ),
  "eye-off": (
    <>
      <path d="m3 3 18 18M9.5 5.3A11 11 0 0 1 12 5c6.5 0 10 7 10 7a18 18 0 0 1-3 4M6 6a18 18 0 0 0-4 6s3.5 7 10 7a11 11 0 0 0 5-1.2M10 10a3 3 0 0 0 4 4" />
    </>
  ),
  tag: (
    <>
      <path d="M3 3h8l10 10-8 8L3 11V3Z" />
      <circle cx="7.5" cy="7.5" r="1" />
    </>
  ),
  copy: (
    <>
      <rect x="8" y="8" width="12" height="13" rx="1" />
      <path d="M16 8V3H3v13h5" />
    </>
  ),
  terminal: (
    <>
      <rect x="2" y="4" width="20" height="16" rx="2" />
      <path d="m6 8 4 4-4 4m7 0h5" />
    </>
  ),
  keyboard: (
    <>
      <rect x="2" y="5" width="20" height="14" rx="2" />
      <path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 12h.01M10 12h.01M14 12h.01M18 12h.01M7 16h10" />
    </>
  ),
  "arrow-up-right": <path d="M6 18 18 6M6 6h12v12" />,
  user: (
    <>
      <circle cx="12" cy="7" r="4" />
      <path d="M4 21v-2a8 8 0 0 1 16 0v2" />
    </>
  ),
  shield: <path d="m12 2 9 4v6c0 5-9 10-9 10S3 17 3 12V6l9-4Z" />,
  "shield-check": (
    <>
      <path d="m12 2 9 4v6c0 5-9 10-9 10S3 17 3 12V6l9-4Z" />
      <path d="m8 12 3 3 5-6" />
    </>
  ),
  "check-square": (
    <>
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <path d="m7 12 3 3 7-7" />
    </>
  ),
  square: <rect x="3" y="3" width="18" height="18" rx="2" />,
  minus: <path d="M5 12h14" />,
  play: <path d="m7 4 14 8-14 8V4Z" />,
  trash: (
    <>
      <path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7m4-7v7" />
    </>
  ),
  network: (
    <>
      <rect x="9" y="2" width="6" height="5" rx="1" />
      <rect x="2" y="17" width="6" height="5" rx="1" />
      <rect x="16" y="17" width="6" height="5" rx="1" />
      <path d="M12 7v5M5 17v-5h14v5" />
    </>
  ),
  link: (
    <>
      <path d="m9 15 6-6m-5 7-3 3a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m2-1 3-3a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0" />
    </>
  ),
  "panel-left": (
    <>
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <path d="M9 3v18" />
    </>
  ),
  upload: (
    <>
      <path d="M12 16V3m-5 5 5-5 5 5M3 16v5h18v-5" />
    </>
  ),
  download: (
    <>
      <path d="M12 3v13m-5-5 5 5 5-5M3 16v5h18v-5" />
    </>
  ),
};

Object.assign(icons, {
  minimize: icons.collapse,
  maximize: icons.expand,
  "git-branch": icons.branch,
  "git-pull-request": icons["pull-request"],
  x: icons.close,
});

export function Icon({ name, size = 16, className, ...props }: IconProps) {
  const shape = Object.prototype.hasOwnProperty.call(icons, name)
    ? icons[name]
    : icons.file;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.65}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      className={className}
      {...props}
    >
      {shape}
    </svg>
  );
}
