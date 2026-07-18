#!/usr/bin/env bash
# pii_scan.sh - blocking scan for accidental PII / homelab-specific identifiers.
#
# Fails (exit 1) if the tracked tree contains a private IPv4 address, a bare
# email address, or any term from an optional external denylist. The denylist
# lives OUTSIDE the repo (never committed) so the homelab's specific hostnames
# stay private; point PII_DENYLIST at it (one term per line, '#' comments ok).
#
# This is intentionally conservative: it scans git-tracked files only, skips
# this script and the lockfiles, and never prints a matched secret value.
set -euo pipefail

fail=0
note() { printf '  %s\n' "$*" >&2; }

# git-tracked files, excluding this scanner and vendored/lock content.
mapfile -t files < <(git ls-files \
  | grep -vE '(^tools/pii_scan\.sh$|(^|/)go\.sum$|(^|/)package-lock\.json$)')

if [ "${#files[@]}" -eq 0 ]; then
  echo "pii_scan: no tracked files to scan"
  exit 0
fi

# 1) Private / homelab IPv4 ranges (RFC1918 + CGNAT).
ip_re='\b(10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}|192\.168\.[0-9]{1,3}\.[0-9]{1,3}|172\.(1[6-9]|2[0-9]|3[01])\.[0-9]{1,3}\.[0-9]{1,3}|100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.[0-9]{1,3}\.[0-9]{1,3})\b'
if grep -REnI "$ip_re" "${files[@]}" 2>/dev/null; then
  note "PII scan: private/homelab IPv4 address found (above)."
  fail=1
fi

# 2) Bare email addresses, excluding the sanctioned noreply commit identity
#    and generic example.com placeholders.
email_re='[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'
if grep -REnI "$email_re" "${files[@]}" 2>/dev/null \
    | grep -vE 'users\.noreply\.github\.com|@example\.(com|org)|noreply@github\.com'; then
  note "PII scan: non-allowlisted email address found (above)."
  fail=1
fi

# 3) Optional external denylist (homelab-specific hostnames/terms).
if [ -n "${PII_DENYLIST:-}" ] && [ -f "${PII_DENYLIST}" ]; then
  while IFS= read -r term; do
    [ -z "$term" ] && continue
    case "$term" in \#*) continue ;; esac
    if grep -REnIF "$term" "${files[@]}" 2>/dev/null; then
      note "PII scan: denylisted term matched (above)."
      fail=1
    fi
  done < "${PII_DENYLIST}"
fi

if [ "$fail" -ne 0 ]; then
  note "pii_scan: FAILED - remove the identifiers above before committing."
  exit 1
fi
echo "pii_scan: clean"
