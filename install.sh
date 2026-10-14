#!/usr/bin/env bash
# Install a published M.I.E.R. release. Compatible with macOS Bash 3.2.
set -euo pipefail

mier_repository=Noah-Wilderom/mier
mier_channel=stable
mier_channel_set=0
mier_portable=0
mier_dry_run=0
mier_prefix=
mier_tmp=
mier_stage=
mier_mac_destination=

die() { printf 'M.I.E.R.: %s\n' "$*" >&2; exit 1; }
usage() {
  cat <<'HELP'
Usage: install.sh [--alpha | --beta | --rc] [--portable] [--prefix DIR] [--dry-run]

Install the latest stable M.I.E.R. release for this OS and CPU architecture.
  --alpha / --beta / --rc  Select only that prerelease channel.
  --portable              Install a user-owned AppImage on Linux.
  --prefix DIR            Linux portable directory or macOS Applications directory.
  --dry-run               Show the release and download without installing.
  --help                  Show this help.

Linux uses apt, pacman, or dnf when available, otherwise a portable AppImage.
macOS installs MIER.app in ~/Applications. Windows uses its per-user setup
installer; run this script in Git Bash/MSYS2/Cygwin.
Requires curl, jq, and sha256sum or shasum. Close M.I.E.R. before upgrading.
HELP
}
cleanup() {
  # Restore the old macOS bundle if installing its replacement failed.
  if [[ -n "$mier_stage" ]]; then
    if [[ -n "$mier_mac_destination" && -e "$mier_stage/previous.app" && ! -e "$mier_mac_destination" ]]; then
      mv "$mier_stage/previous.app" "$mier_mac_destination" || return
    fi
    rm -rf "$mier_stage"
  fi
  if [[ -n "$mier_tmp" ]]; then rm -rf "$mier_tmp"; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

while [[ $# -gt 0 ]]; do
  case "$1" in
    --alpha|--beta|--rc)
      [[ "$mier_channel_set" == 0 ]] || die 'Choose just one release channel.'
      mier_channel=${1#--}; mier_channel_set=1 ;;
    --portable) mier_portable=1 ;;
    --prefix)
      [[ $# -ge 2 && -n "$2" && "$2" != --* ]] || die '--prefix needs a directory.'
      mier_prefix=$2; mier_portable=1; shift ;;
    --dry-run) mier_dry_run=1 ;;
    --help|-h) usage; exit 0 ;;
    *) die "Unknown option: $1. Use --help for usage." ;;
  esac
  shift
done

for mier_tool in curl jq; do
  command -v "$mier_tool" >/dev/null 2>&1 || die "$mier_tool is required. Install it with your system's package manager."
done
mier_system=$(uname -s)
mier_machine=$(uname -m)
case "$mier_system" in
  Linux) mier_os=linux ;;
  Darwin)
    mier_os=darwin
    # An Intel shell under Rosetta should still install the ARM64 bundle.
    if [[ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" == 1 ]]; then mier_machine=arm64; fi ;;
  MINGW*|MSYS*|CYGWIN*)
    mier_os=windows
    mier_machine=${PROCESSOR_ARCHITEW6432:-${PROCESSOR_ARCHITECTURE:-$mier_machine}} ;;
  *) die "Unsupported OS: $mier_system. Supported: Linux, macOS, Windows." ;;
esac
case "$mier_machine" in
  x86_64|amd64|AMD64) mier_arch=amd64 ;;
  arm64|aarch64|ARM64) mier_arch=arm64 ;;
  *) die "Unsupported CPU architecture: $mier_machine. Supported: AMD64 and ARM64." ;;
esac
if [[ "$mier_os" == windows && "$mier_portable" == 1 ]]; then
  die '--portable and --prefix apply to Linux and macOS; Windows uses its per-user setup installer.'
fi

mier_method=$mier_os
if [[ "$mier_os" == linux ]]; then
  mier_method=appimage
  if [[ "$mier_portable" == 0 ]]; then
    if command -v pacman >/dev/null 2>&1; then mier_method=pacman
    elif command -v dnf >/dev/null 2>&1; then mier_method=dnf
    elif command -v apt-get >/dev/null 2>&1; then mier_method=apt
    fi
  fi
fi
case "$mier_method" in
  apt) mier_extension=deb ;;
  pacman) mier_extension=pkg.tar.zst ;;
  dnf) mier_extension=rpm ;;
  appimage) mier_extension=AppImage ;;
  darwin) mier_extension=zip ;;
  windows) mier_extension=exe ;;
esac

umask 077
mier_tmp=$(mktemp -d "${TMPDIR:-/tmp}/mier-install.XXXXXXXX")
fetch() {
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    --connect-timeout 15 --max-time "${3:-600}" --retry 2 \
    --output "$2" "$1"
}
printf 'Finding the latest %s release for %s/%s…\n' "$mier_channel" "$mier_os" "$mier_arch"
printf '[]\n' > "$mier_tmp/releases.json"
mier_page=1
while :; do
  fetch "https://api.github.com/repos/$mier_repository/releases?per_page=100&page=$mier_page" "$mier_tmp/page.json" 60 \
    || die 'Could not fetch releases. Check your connection and GitHub API rate limits.'
  jq -e 'type == "array"' "$mier_tmp/page.json" >/dev/null || die 'GitHub returned an invalid release list.'
  jq -s 'add' "$mier_tmp/releases.json" "$mier_tmp/page.json" > "$mier_tmp/next.json"
  mv "$mier_tmp/next.json" "$mier_tmp/releases.json"
  if [[ $(jq 'length' "$mier_tmp/page.json") -lt 100 ]]; then break; fi
  mier_page=$((mier_page + 1))
  [[ "$mier_page" -le 100 ]] || die 'Release pagination exceeded 100 pages.'
done

# Sort versions numerically, including alpha.10 versus alpha.2. Ignore build
# metadata in precedence and exclude drafts and other prerelease channels.
jq --arg channel "$mier_channel" '
  def version:
    capture("^v?(?<major>0|[1-9][0-9]*)\\.(?<minor>0|[1-9][0-9]*)\\.(?<patch>0|[1-9][0-9]*)(?:-(?<pre>[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*))?(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$")
    | select([.major,.minor,.patch] | all(tonumber <= 65535))
    | select((.pre // "" | split(".")) | all(test("^[0-9]+$") | not) or all(test("^0[0-9]+$") | not));
  [.[] | select(.draft == false and .published_at != null)
    | . as $release | (.tag_name | version) as $v
    | select(if $channel == "stable" then .prerelease == false and ($v.pre // "") == ""
             else .prerelease == true and (($v.pre // "" | split(".")[0]) == $channel) end)
    | {release: $release, order: [($v.major | tonumber), ($v.minor | tonumber), ($v.patch | tonumber),
        (($v.pre // "" | split(".")) | map(if test("^[0-9]+$") then [0, tonumber] else [1, .] end)),
        .published_at]}]
  | sort_by(.order) | last | .release // null
' "$mier_tmp/releases.json" > "$mier_tmp/release.json"
jq -e 'type == "object"' "$mier_tmp/release.json" >/dev/null \
  || die "No $mier_channel release is available. Use --alpha, --beta, or --rc only if you want that prerelease channel."
mier_tag=$(jq -r '.tag_name' "$mier_tmp/release.json")
mier_version=${mier_tag#v}
mier_asset="mier-$mier_version-$mier_os-$mier_arch.$mier_extension"
if [[ "$mier_os" == windows ]]; then mier_asset="mier-$mier_version-windows-$mier_arch-setup.exe"; fi
asset_url() {
  jq -er --arg name "$1" '[.assets[] | select(.name == $name and .state == "uploaded") | .browser_download_url] | if length == 1 then .[0] else error("Missing or duplicate asset") end' "$mier_tmp/release.json"
}
mier_download=$(asset_url "$mier_asset") || die "Release $mier_tag has no $mier_asset download. Its packaging may still be running."
mier_checksums=$(asset_url SHA256SUMS) || die "Release $mier_tag has no checksums yet. Retry when packaging finishes."
asset_url manifest.json >/dev/null || die "Release $mier_tag is still being published. Retry when packaging finishes."
for mier_url in "$mier_download" "$mier_checksums"; do
  [[ "$mier_url" == "https://github.com/$mier_repository/releases/download/"* ]] || die 'Unexpected release download URL.'
done
printf 'Release: %s\nInstall method: %s\nDownload: %s\n' "$mier_tag" "$mier_method" "$mier_download"
if [[ "$mier_dry_run" == 1 ]]; then exit 0; fi

if command -v sha256sum >/dev/null 2>&1; then mier_hasher=sha256sum
elif command -v shasum >/dev/null 2>&1; then mier_hasher=shasum
else die 'sha256sum or shasum is required to verify the download.'
fi
mier_file="$mier_tmp/$mier_asset"
fetch "$mier_checksums" "$mier_tmp/SHA256SUMS" || die 'Could not download release checksums.'
fetch "$mier_download" "$mier_file" || die 'Could not download the selected installer.'
mier_expected=$(awk -v name="$mier_asset" '$2 == name {print tolower($1)}' "$mier_tmp/SHA256SUMS")
[[ "$mier_expected" =~ ^[a-f0-9]{64}$ ]] || die 'The selected download has no unambiguous SHA-256 checksum.'
if [[ "$mier_hasher" == shasum ]]; then mier_actual=$(shasum -a 256 "$mier_file" | awk '{print $1}')
else mier_actual=$(sha256sum "$mier_file" | awk '{print $1}')
fi
[[ "$mier_actual" == "$mier_expected" ]] || die 'SHA-256 verification failed. Nothing was installed.'
printf 'SHA-256 verified.\n'
as_root() {
  if [[ $(id -u) == 0 ]]; then "$@"
  else
    command -v sudo >/dev/null 2>&1 || die 'sudo is required for a system package. Use --portable for a user-owned install.'
    sudo "$@"
  fi
}
case "$mier_method" in
  apt|pacman|dnf)
    # Package-manager sandbox users may need to read this public download.
    chmod 755 "$mier_tmp"; chmod 644 "$mier_file"
    case "$mier_method" in
      apt) as_root apt-get install -y "$mier_file" ;;
      pacman) as_root pacman -U --noconfirm "$mier_file" ;;
      dnf) as_root dnf install -y "$mier_file" ;;
    esac
    printf 'Installed %s. Launch M.I.E.R. from your desktop or run mier.\n' "$mier_tag" ;;
  appimage)
    mier_custom_prefix=$mier_prefix
    mier_prefix=${mier_prefix:-"${XDG_DATA_HOME:-$HOME/.local/share}/mier"}
    mkdir -p "$mier_prefix"
    mier_prefix=$(cd "$mier_prefix" && pwd)
    mier_stage=$(mktemp -d "$mier_prefix/.mier-install.XXXXXXXX")
    install -m 755 "$mier_file" "$mier_stage/mier.AppImage"
    printf '#!/usr/bin/env bash\nAPPIMAGE_EXTRACT_AND_RUN=1 exec %q "$@"\n' "$mier_prefix/mier.AppImage" > "$mier_stage/mier"
    chmod 755 "$mier_stage/mier"
    mv -f "$mier_stage/mier.AppImage" "$mier_prefix/mier.AppImage"
    mv -f "$mier_stage/mier" "$mier_prefix/mier"
    if [[ -z "$mier_custom_prefix" ]]; then
      mkdir -p "$HOME/.local/bin"
      ln -sf "$mier_prefix/mier" "$HOME/.local/bin/mier"
    fi
    printf 'Installed %s. Launch with %s/mier\n' "$mier_tag" "$mier_prefix" ;;
  darwin)
    command -v ditto >/dev/null 2>&1 || die 'The macOS ditto utility is required.'
    ditto -x -k "$mier_file" "$mier_tmp/extracted"
    [[ -x "$mier_tmp/extracted/MIER.app/Contents/MacOS/mag-ik-een-review" ]] || die 'The download does not contain a complete MIER.app bundle.'
    mier_prefix=${mier_prefix:-"$HOME/Applications"}
    mkdir -p "$mier_prefix"
    mier_stage=$(mktemp -d "$mier_prefix/.mier-install.XXXXXXXX")
    ditto "$mier_tmp/extracted/MIER.app" "$mier_stage/MIER.app"
    mier_mac_destination="$mier_prefix/MIER.app"
    if [[ -e "$mier_mac_destination" ]]; then mv "$mier_mac_destination" "$mier_stage/previous.app"; fi
    mv "$mier_stage/MIER.app" "$mier_mac_destination"
    printf 'Installed %s at %s. Open MIER.app in Finder.\n' "$mier_tag" "$mier_mac_destination" ;;
  windows)
    chmod +x "$mier_file"
    # Prevent MSYS from rewriting NSIS /S into a Unix-looking path.
    MSYS2_ARG_CONV_EXCL='*' "$mier_file" /S
    printf 'Installed %s. Open M.I.E.R. from the Start menu.\n' "$mier_tag" ;;
esac
