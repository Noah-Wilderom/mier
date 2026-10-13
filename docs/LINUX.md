# Linux and Omarchy installation

Every published release builds Linux AMD64 and ARM64 downloads. Choose `amd64` for Intel/AMD (`uname -m` reports `x86_64`) or `arm64` for ARM (`aarch64`). Packages install M.I.E.R. into the desktop application launcher and provide the `mier` terminal command.

| System | Download | Install |
| --- | --- | --- |
| Omarchy, Arch Linux, and derivatives | `.pkg.tar.zst` | `sudo pacman -U ./mier-VERSION-linux-ARCH.pkg.tar.zst` |
| Debian 13+, Ubuntu 24.04+, and derivatives | `.deb` | `sudo apt install ./mier-VERSION-linux-ARCH.deb` |
| Current Fedora releases | `.rpm` | `sudo dnf install ./mier-VERSION-linux-ARCH.rpm` |
| Portable desktop application | `.AppImage` | `chmod +x ./mier-VERSION-linux-ARCH.AppImage`, then run it |
| Portable executable with in-app updates | `.tar.gz` | Extract into a writable user directory and run `mag-ik-een-review` |

The native packages declare Git, GTK 4, WebKitGTK 6, and a glibc 2.39 minimum. The initial build baseline is Ubuntu 24.04; older distributions such as Debian 12, Ubuntu 22.04, and RHEL 9 need a separate legacy build. AppImages bundle GTK/WebKit libraries and subprocesses but still use the host kernel, glibc, Git, and desktop services. They are not a compatibility layer for older Linux systems.

## Omarchy from a GitHub release

There is no special Omarchy application format: use the native Arch package. With GitHub CLI installed, download the latest release into a fresh directory and let pacman install its dependencies:

```sh
# Change to arm64 for an aarch64 machine.
mier_arch=amd64
mier_download_dir=$(mktemp -d)
gh release download --repo Noah-Wilderom/mier \
  --pattern "mier-*-linux-${mier_arch}.pkg.tar.zst" \
  --dir "$mier_download_dir"
sudo pacman -U "$mier_download_dir"/*.pkg.tar.zst
mier
```

This command becomes available after the first release containing these packages is published. Repeating the download and install steps upgrades a direct GitHub installation. Packages are initially unsigned, as are the Windows and macOS installers; release assets include `SHA256SUMS`.

## AUR and Omarchy's package CLI

The release also contains `mier-VERSION-aur.tar.gz`. It holds a `mier-bin` PKGBUILD and matching `.SRCINFO`, with separate SHA-256-pinned sources for `x86_64` and `aarch64`. On Arch, unpack the recipe and run:

```sh
makepkg -si
```

To enable installation by package name, a maintainer must publish the generated recipe to the AUR and refresh it on each release. Confirm the project license first; current metadata uses `unknown` because the repository has no `LICENSE` file. The package is not registered in the AUR by this change.

Once `mier-bin` is published to the AUR, Omarchy 4 supports:

```sh
omarchy pkg aur add mier-bin
```

The script form, also available in earlier Omarchy versions, is `omarchy-pkg-aur-add mier-bin`. The regular `omarchy pkg add mier` command requires the package to be hosted in a configured pacman repository; a GitHub release alone does not make it discoverable there.

## Desktop integration and updates

The package owns `/usr/bin/mag-ik-een-review`, the `mier` command alias, `/usr/share/applications/mag-ik-een-review.desktop`, and its icon in `/usr/share/pixmaps`. Omarchy's launcher discovers the desktop entry automatically. The DEB also installs a scoped AppArmor profile for that executable, allowing WebKitGTK's sandbox namespaces under Ubuntu's namespace restriction. Native file dialogs use the installed desktop portal. A Secret Service keyring such as GNOME Keyring supports saved GitHub tokens; GitHub CLI authentication is an alternative.

Use pacman/AUR, apt, or dnf to update package installations. M.I.E.R. disables in-app binary replacement for system package paths and AppImage mounts. The writable portable `.tar.gz` installation retains the signed Wails update flow. AppImages are upgraded by downloading and replacing the whole AppImage.

## Building and checking packages

On a native Linux runner with the [Wails build dependencies](https://v3.wails.io/guides/build/linux/), Node, and Go:

```sh
node scripts/release.mjs package linux amd64 0.1.0
python3 scripts/verify-linux-packages.py dist/installers amd64 0.1.0
```

Use `arm64` on a native ARM64 runner. Packaging also needs `file`, `rpm`, `bsdtar` (`libarchive-tools` on Debian/Ubuntu), and `desktop-file-validate`. The Wails CLI includes nFPM, so no separate DEB/RPM/Arch packager is required. The AppImage builder downloads linuxdeploy/AppImage tooling and runs it without FUSE in CI.

References: [Omarchy packages](https://omarchy.org/manual/other-packages/), [Omarchy AUR helper](https://github.com/omacom/omarchy/blob/quattro/bin/omarchy-pkg-aur-add), [Wails Linux packaging](https://v3.wails.io/guides/build/linux/), [nFPM configuration](https://nfpm.goreleaser.com/docs/configuration/), [Arch PKGBUILD](https://wiki.archlinux.org/title/PKGBUILD), and [.SRCINFO](https://wiki.archlinux.org/title/.SRCINFO).
