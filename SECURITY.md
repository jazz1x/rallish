# Security Policy

## Supported Versions

The latest tagged release is supported. Older versions receive no
backport fixes.

## Reporting a Vulnerability

Email **security@jazz1x.dev**
with a description of the issue and reproduction steps. Please do **not**
file public GitHub issues for security-sensitive reports.

We will acknowledge within 72 hours and target a fix in the next patch
release.

## Threat Model

See [DESIGN.md §14](DESIGN.md#14-security--threat-model) for the
documented threat model and the security controls in place
(`forbidigo` lint, path-traversal guards via `internal/safepath`,
Unix-socket file permissions `0600`, no bearer-token auth in v0,
`govulncheck` on every PR, cosign keyless signing on releases).
