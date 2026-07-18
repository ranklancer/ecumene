# Security Policy

Juridical is an authorization and audit control plane; the security of
Juridical itself is a first-class concern.

## Supported versions

Juridical is pre-1.0 and ships from `main`. Security fixes are applied to
`main` and the latest tagged release.

## Reporting a vulnerability

Please report suspected vulnerabilities **privately** - do not open a public
issue, pull request, or discussion.

Use GitHub's **private vulnerability reporting** for this repository:

1. Open the repository's **Security** tab.
2. Choose **Report a vulnerability**.
3. Include: affected component/version/commit, impact, reproduction steps or a
   proof of concept, and any known mitigations.

This channel is private to the maintainers and keeps the report out of public
view until a fix is available.

## Response targets (SLAs)

Good-faith targets, not contractual guarantees:

| Stage | Target |
|---|---|
| Acknowledge receipt | within 3 business days |
| Triage / initial assessment | within 7 business days |
| Fix or mitigation plan (confirmed issue) | within 30 days, severity-dependent |
| Coordinated public disclosure | by mutual agreement after a fix ships |

Critical, actively-exploited issues are prioritized over these windows.

## Safe harbor

We support good-faith security research and will not pursue or support legal
action against researchers who:

- make a good-faith effort to avoid privacy violations, data destruction, and
  service interruption;
- only interact with systems and data they own or are authorized to test;
- report promptly and give us reasonable time to remediate before disclosure;
- do not access or exfiltrate more data than necessary to demonstrate the
  issue.

Activity that violates other laws or harms users or third parties is outside
this safe harbor. We are happy to credit reporters who wish to be acknowledged.

## Scope

In scope: the Juridical source in this repository and the release artifacts
published from it. Out of scope: third-party dependencies (report upstream)
and issues requiring privileged local access Juridical does not itself grant.
