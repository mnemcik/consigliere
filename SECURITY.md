# Security Policy

## Supported Versions

Consigliere is a solo-maintained project. Only the latest tagged release receives security fixes.

| Version | Supported |
| --- | --- |
| Latest release (see [Releases](https://github.com/mnemcik/consigliere/releases)) | Yes |
| Older versions | No |

## Reporting a Vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately via GitHub's Security Advisories:

https://github.com/mnemcik/consigliere/security/advisories/new

You'll get an initial response on a best-effort basis, typically within 7 days. If the report is accepted, a fix and coordinated disclosure timeline will be discussed with you directly in the advisory thread.

If the advisories page is unavailable for any reason, a GitHub issue titled `security: please contact me privately` (without any vulnerability details) is acceptable as a fallback signal.

## Scope

In scope:

- The `cg` binary and anything it does at runtime (file reads/writes, network calls, subprocess invocations)
- The release artefacts published on GitHub Releases
- The embedded template set shipped inside the binary

Out of scope:

- Vulnerabilities in Go's standard library or third-party dependencies (report those upstream; we will pick up fixes through Dependabot)
- Issues caused by user modifications to generated workspace content
- Social engineering or physical attacks against maintainers
