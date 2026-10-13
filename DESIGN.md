# M.I.E.R. design system

## Direction

The user approved the generated Review Inbox reference (direction B). Use flat, quiet desktop surfaces, a compact left sidebar, a master-detail pull request list, and a file tree beside readable code. The primary task is careful code review. An expanded Changes workspace hides navigation and the inbox without losing review state.

## Initial reference

The original Repository workspace concept (A), supplied again for the handoff, is preserved unchanged below. It establishes repository context, a PR list next to discussion, a metadata rail, restrained green actions, and native desktop density. The OpenGit name and sample labels in the image are historical; the application is now M.I.E.R. The later Review Inbox direction above describes the current layout. Some illustrated concepts, including reactions, projects, and milestones, are not implemented controls.

![Initial repository workspace concept A](docs/design/initial-repository-workspace.png)

See the [research index](docs/RESEARCH.md) for source references and implementation experiments.

## Appearance

Follow the system light/dark theme with explicit System, Light, and Dark overrides. Light mode is intended for developers reviewing code in a well-lit work environment; dark mode follows the same hierarchy for evening work. Restrained color: green for actions and approvals, blue for context, red for removals and failures, amber for waiting states. Pair colors with icons and text.

## Typography

Use the operating system sans-serif stack for navigation, labels, and prose. Use the operating system monospace stack for code, branches, and commit hashes. UI labels are 12–14px; code defaults to 13px with generous line spacing and is configurable from 12–20px; pull request titles are 21px. Code and conversation text remain selectable.

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

Local and PR code use the same grammar highlighting and independent light/dark syntax theme preferences. Each appearance offers four cards with real code previews. Language intelligence has a quiet availability indicator; missing tools link to configuration rather than failing every hover. Definition navigation opens a read-only file pane and retains the underlying review state.

Editor actions use a main launch button and adjacent selector with the official editor logos. The GitHub action is a separate button. Comment headers use profile pictures with initials as a fallback, a Bot badge without the literal [bot] suffix, and an Author badge when the commenter opened the PR.
