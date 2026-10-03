#!/usr/bin/env python3
"""Build public archives from an explicit allowlist; never include user state."""
import hashlib
from pathlib import Path
import struct
import zipfile

VERSION = "1.3"
ROOT = Path(__file__).resolve().parent
SOURCE_FILES = (
    ".gitignore", "README.md", "README.ja.md", "PRIVACY.md", "CHANGELOG.md",
    "RELEASING.md", "LICENSE", "GO-LICENSE.txt", "THIRD_PARTY_NOTICES.md",
    "go.mod", "build.cmd", "package_release.py",
    "main_windows.go", "worker_windows.go", "winapi_windows.go",
    "protocol.go", "smf.go", "rename.go", "rename_windows.go",
    "layout.go", "layout_windows.go", "settings.go",
    "protocol_test.go", "rename_test.go", "layout_test.go", "settings_test.go",
)
PORTABLE_DOCS = ("README.md", "README.ja.md", "PRIVACY.md", "LICENSE", "GO-LICENSE.txt", "THIRD_PARTY_NOTICES.md", "CHANGELOG.md")

def write_zip(path, entries):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        for name, source in entries:
            if source.is_symlink() or not source.is_file():
                raise RuntimeError("Missing file or symlink: " + source.name)
            info = zipfile.ZipInfo(name, (2026, 10, 3, 0, 0, 0))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o100644 << 16
            archive.writestr(info, source.read_bytes())
    with zipfile.ZipFile(path) as archive:
        if archive.testzip() is not None:
            raise RuntimeError("Archive verification failed")

def main():
    dist = ROOT / "dist"
    dist.mkdir(exist_ok=True)
    exe = dist / f"Finalizer_Librarian_v{VERSION}.exe"
    data = exe.read_bytes()
    if data[:2] != b"MZ" or len(data) < 64:
        raise RuntimeError("Build the Windows executable first")
    offset = struct.unpack_from("<I", data, 0x3C)[0]
    if data[offset:offset+4] != b"PE\0\0" or struct.unpack_from("<H", data, offset+4)[0] != 0x8664:
        raise RuntimeError("Expected an x64 Windows PE executable")
    # Catch common accidental builds with local paths or embedded VCS settings.
    for marker in (b"/workspace/", b"/root/", b"/Users/", b"vcs.revision=", b"vcs.time="):
        if marker in data:
            raise RuntimeError("Executable contains build metadata; rebuild with README flags")
    source = dist / f"Finalizer_Librarian_v{VERSION}_source.zip"
    portable = dist / f"Finalizer_Librarian_v{VERSION}_Windows_x64.zip"
    write_zip(source, [(name, ROOT / name) for name in SOURCE_FILES])
    prefix = f"Finalizer_Librarian_v{VERSION}/"
    write_zip(portable, [(prefix+exe.name, exe)] + [(prefix+name, ROOT/name) for name in PORTABLE_DOCS])
    (dist / "SHA256SUMS.txt").write_text("".join(hashlib.sha256(p.read_bytes()).hexdigest()+"  "+p.name+"\n" for p in (exe,source,portable)), encoding="utf-8")
    print("Source and portable archives written to dist.")

if __name__ == "__main__":
    main()
