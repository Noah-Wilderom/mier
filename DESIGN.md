# M.I.E.R. design system

## Direction

The user approved the generated Review Inbox reference (direction B). Use flat, quiet desktop surfaces, a compact left sidebar, a master-detail pull request list, and a file tree beside readable code. The primary task is careful code review. An expanded Changes workspace hides navigation and the inbox without losing review state.

## Appearance

Follow the system light/dark theme with explicit System, Light, and Dark overrides. Light mode is intended for developers reviewing code in a well-lit work environment; dark mode follows the same hierarchy for evening work. Restrained color: green for actions and approvals, blue for context, red for removals and failures, amber for waiting states. Pair colors with icons and text.

## Typography

Use the operating system sans-serif stack for navigation, labels, and prose. Use the operating system monospace stack for code, branches, and commit hashes. UI labels are 12–14px; code is 12–13px with generous line spacing; pull request titles are 21px. Code and conversation text remain selectable.

## Layout

At desktop sizes the sidebar is approximately 220px and the inbox 300px. The review file tree is adjustable, initially around 200px. The diff fills the remaining width and scrolls internally. Thin one-pixel dividers establish panes. Avoid decorative cards and colored side stripes. Expanded review uses the full available window width. Collapse panes structurally on narrower screens.

## Controls

Use compact 30–34px buttons, consistent 16px stroke icons, six-pixel control radii, visible keyboard focus, and clear disabled states. Use inline forms for PR creation, discussions, and review submission. The command palette is a focused overlay. Native window decorations and application menus come from Wails.

## Review interactions

Split and unified diffs, optional whitespace filtering, file search, viewed-file progress, inline pending comments, replies, thread resolution, review decisions, and merge readiness. Expand with F and return with Escape. Navigation shortcuts use Cmd/Ctrl. Preserve selected files and pending review work when expanding. Motion is limited to short feedback transitions and respects reduced motion.

## Live data and previews

The desktop app operates on local Git repositories and authenticated GitHub data. A persistent status bar shows sync state. Repository opening and initial pull request data use skeletons; background refresh keeps existing content readable. Repository names truncate on one line with their full identity available in tooltips and a searchable repository selector. Comments render safe GitHub-flavored Markdown and Unicode emoji.

New commits preserve the current code revision and pending review work until Refresh changes is selected. Local draft comments survive reopening and retain their original commit anchors. An explicitly labelled browser/demo workspace remains available for exploring the interface. Jira installation is currently a three-second backend preview.

Notification preferences belong to each GitHub repository. All PR event types and in-app toasts default on; desktop notifications default off. Importing existing data never creates notification floods.
