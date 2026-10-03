# Building a release

1. Run `go test ./...` in the repository. On Windows, `build.cmd` runs tests and builds the executable.
2. Build the Windows x64 executable in `dist/Finalizer_Librarian_v1.3.exe` using the README command.
3. Run `python package_release.py` with Python 3. The packager checks the Windows PE header and copies only explicitly allowed files.
4. Publish the repository source files to the repository root. Publish the portable ZIP and SHA256SUMS.txt as release assets; the source ZIP is optional.

Do not upload an entire development workspace or application data directory.
Do not add MIDI captures, production presets, local settings, logs or crash dumps.
The ignore rules do not remove files already tracked by Git. Check the actual
staged files and archive contents before publishing.

The source archive has no Git history or user-specific Git configuration.
The build excludes VCS metadata and local source paths. The release executable
is unsigned unless the publisher signs it. If signing, do so before packaging
so the archive and checksums reflect the signed bytes.

This repository uses the MIT License with a project-level contributor credit;
no personal identity is required in the copyright line. The included Go notice
must accompany binaries. No code-signing certificate or private key belongs in
the repository. Review license choices and signing requirements as part of
maintaining your own distribution.
