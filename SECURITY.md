# Security Policy

## Supported versions

TermCall is pre-release software. Security fixes are made on `main` and, after
publication, on the latest `v0.1.0-rc.*` prerelease only. Older commits and
locally built binaries are unsupported.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability.

Use GitHub's **Report a vulnerability** button on the repository's Security
tab. Private vulnerability reporting is the preferred channel and lets us
coordinate a fix before disclosure. If that button is unavailable, contact the
repository owner privately through their GitHub profile and include a link to
this policy.

Please include:

- affected version, commit, or container digest;
- a minimal reproducible proof of concept;
- impact and realistic attack prerequisites;
- suggested mitigation, if known; and
- whether you are willing to be credited.

Do not include TURN secrets, private keys, access tokens, personal call data, or
unredacted logs in a report.

## Response targets

We aim to acknowledge reports within seven days and provide a status update
within 14 days. Timelines for a fix depend on severity and reproducibility.
We will coordinate disclosure with the reporter and publish a GitHub Security
Advisory when a fix is available.

## Scope

The client, signaling server, TURN configuration, release workflow, and
published server images are in scope. Third-party dependencies should also be
reported here when they are reachable through TermCall.
