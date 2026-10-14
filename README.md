# M.I.E.R.

![M.I.E.R. pull request review in light and dark mode](art/mier-preview.png)

**Mag ik een review?** A desktop workspace for Git repositories and GitHub pull requests, built with Wails 3, Go, React, and TypeScript.

Manage branches and local changes, compare commits, and review pull requests with inline comments, syntax highlighting, and light or dark appearance.

Install and update PHP, TypeScript/JavaScript, and Vue language servers from Settings for symbol information and go to definition.

[Download the latest release](https://github.com/Noah-Wilderom/mier/releases/latest) · macOS, Windows, and Linux · Intel/AMD and ARM64

[Linux and Omarchy installation](docs/LINUX.md) — DEB, RPM, Arch packages, and AppImage.

## Development

Install Go, Node.js, Git, and the [Wails platform dependencies](https://v3.wails.io/getting-started/installation/), then run:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
wails3 doctor
wails3 dev
```

[Development guide](docs/DEVELOPMENT.md) · [Releasing and updates](docs/RELEASING.md) · [Architecture](docs/ARCHITECTURE.md)

M.I.E.R. is currently unlicensed.
