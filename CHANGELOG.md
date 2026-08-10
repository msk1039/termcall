# Changelog

All notable changes are documented here. TermCall follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and uses semantic
versioning for published tags.

## [Unreleased]

### Added

- Community, security, and release-process documentation.

## [0.1.0-rc.1] - 2026-08-10

### Added

- Hardened signaling, TURN credentials, bounded protocol validation, and
  automated security checks.
- Non-root server container, SBOM, checksums, provenance, and release workflow.
- Camera and microphone consent defaults, safe client lifecycle behavior, and
  fixed-size camera-off placeholders.

### Changed

- Rooms use server-generated UUIDs and a limited full-mesh membership model.
- Default signaling uses TLS (`wss`) for the public deployment.

[Unreleased]: https://github.com/msk1039/termcall/compare/v0.1.0-rc.1...HEAD
[0.1.0-rc.1]: https://github.com/msk1039/termcall/releases/tag/v0.1.0-rc.1
