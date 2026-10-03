# Changelog

## 1.3 - 2026-10-03

- Prepare a public source distribution and Windows x64 portable package.
- Provide standalone English and Japanese documentation.
- Stop automatic recovery-bank writes and reads; banks are saved only on request.
- Persist MIDI port identifiers as hashes instead of plaintext labels.
- Use synthetic tests with no dependency on captured user banks.
- Package from an explicit file allowlist and omit logs, state and build caches.
- Include MIT licensing and Go runtime redistribution notices.

Features include complete RAM-bank backup/restore, a four-column preset list,
independent MIDI channels, double-click recall, inline renaming, pending-name
markers, text export, responsive layout and an isolated MIDI worker.
