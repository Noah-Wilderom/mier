#!/usr/bin/env python3
"""Inspect actual release packages without installing onto the build machine."""

import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys
import tempfile


def run(*args, cwd=None):
    return subprocess.check_output(args, cwd=cwd, text=True, stderr=subprocess.PIPE)


def verify(directory, arch, version):
    if arch not in ("amd64", "arm64") or not re.fullmatch(r"\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?", version):
        raise ValueError("Expected amd64/arm64 and a release version")
    directory = Path(directory).resolve()
    numeric_version = re.match(r"\d+\.\d+\.\d+", version)[0]
    native_arch = "x86_64" if arch == "amd64" else "aarch64"
    dependencies = {
        "deb": ("git", "libc6 (>= 2.39)", "libgtk-4-1 (>= 4.14)", "libwebkitgtk-6.0-4"),
        "rpm": ("git", "glibc >= 2.39", "gtk4 >= 4.14", "webkitgtk6.0"),
        "pkg.tar.zst": ("git", "glibc>=2.39", "gtk4>=4.14", "webkitgtk-6.0"),
    }
    for extension in ("deb", "rpm", "pkg.tar.zst", "AppImage"):
        package = directory / f"mier-{version}-linux-{arch}.{extension}"
        if not package.is_file():
            raise ValueError(f"Missing package: {package}")
        with tempfile.TemporaryDirectory(prefix="mier-package-check-") as temporary:
            root = Path(temporary)
            if extension == "deb":
                name = run("dpkg-deb", "--field", str(package), "Package").strip()
                package_version = run("dpkg-deb", "--field", str(package), "Version").strip()
                package_arch = run("dpkg-deb", "--field", str(package), "Architecture").strip()
                assert name == "mier" and package_arch == arch and package_version.startswith(numeric_version)
                metadata = run("dpkg-deb", "--field", str(package), "Depends")
                run("dpkg-deb", "--extract", str(package), temporary)
                profile = root / "etc/apparmor.d/mier"
                assert profile.is_file() and "/usr/bin/mag-ik-een-review" in profile.read_text()
                if shutil.which("apparmor_parser"):
                    run("apparmor_parser", "--skip-kernel-load", "--skip-cache", str(profile))
            elif extension == "rpm":
                metadata = run("rpm", "-qp", "--requires", str(package))
                fields = run("rpm", "-qp", "--queryformat", "%{NAME} %{VERSION} %{ARCH}", str(package)).split()
                assert fields[0] == "mier" and fields[1].startswith(numeric_version) and fields[2] == native_arch
                run("bsdtar", "-xf", str(package), "-C", temporary)
            elif extension == "pkg.tar.zst":
                run("bsdtar", "-xf", str(package), "-C", temporary)
                metadata = (root / ".PKGINFO").read_text()
                assert "pkgname = mier\n" in metadata and f"arch = {native_arch}\n" in metadata
                assert f"pkgver = {numeric_version}" in metadata
            else:
                os.chmod(package, 0o755)
                run(str(package), "--appimage-extract", cwd=temporary)
                root /= "squashfs-root"
                metadata = ""
                assert (root / "AppRun").is_file()
                assert (root / "mag-ik-een-review.desktop").is_file()
                assert (root / ".DirIcon").is_file()
                assert list(root.glob("usr/lib/**/WebKitWebProcess")), "Missing bundled WebKit subprocess"

            for dependency in dependencies.get(extension, ()):
                assert dependency in metadata, (extension, dependency)
            binary = root / "usr/bin/mag-ik-een-review"
            header = binary.read_bytes()[:64]
            assert header[:6] == b"\x7fELF\x02\x01" and struct.unpack_from("<H", header, 18)[0] == (62 if arch == "amd64" else 183)
            assert binary.stat().st_mode & 0o111, "Executable permissions were lost"
            if extension != "AppImage":
                alias = root / "usr/bin/mier"
                assert alias.is_symlink() and Path(os.readlink(alias)).name == binary.name
                desktop = root / "usr/share/applications/mag-ik-een-review.desktop"
                assert (root / "usr/share/pixmaps/mag-ik-een-review.png").is_file()
                run("desktop-file-validate", str(desktop))
            print(f"Verified {package.name}: {arch} executable, metadata, dependencies, and desktop files")


if __name__ == "__main__":
    verify(*sys.argv[1:])
