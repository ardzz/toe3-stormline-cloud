#!/usr/bin/env bash
# Usage: smoke.sh BASE_URL
# Checks a running server end to end: health, registration, upload, download and an unauthenticated request. The
# server must have open registration. Extra curl options, such as -k for a test certificate, come from CURL_OPTS.
set -euo pipefail

base=${1:?usage: smoke.sh https://host[:port]}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# shellcheck disable=SC2086 # CURL_OPTS holds several options
req() { curl -sS ${CURL_OPTS:-} "$@"; }
fail() {
	echo "smoke: $*" >&2
	exit 1
}

for _ in $(seq 1 60); do
	if req -f -o "$tmp/health" "$base/v1/health" 2>/dev/null; then
		break
	fi
	sleep 2
done
grep -q '"service":"toe3-cloud"' "$tmp/health" 2>/dev/null || fail "no healthy server at $base"

user="smoke$(date +%s)"
req -f -X POST "$base/v1/auth/register" -H 'Content-Type: application/json' \
	-d "{\"username\":\"$user\",\"password\":\"smoke-test-password\",\"device\":\"smoke\"}" -o "$tmp/session" ||
	fail "registration failed (is TOE3_REGISTRATION=open?)"
token=$(sed -n 's/.*"token":"\([^"]*\)".*/\1/p' "$tmp/session")
[ -n "$token" ] || fail "registration returned no token"

printf '{"format":"toe3-prefs-snapshot","version":1,"prefs":{}}' | gzip -n >"$tmp/snapshot.gz"
sha=$(sha256sum "$tmp/snapshot.gz" | cut -d' ' -f1)
req -f -X PUT "$base/v1/saves" -H "Authorization: Bearer $token" -H 'If-None-Match: *' \
	-H 'Content-Type: application/gzip' -H "X-Toe3-Sha256: $sha" -H 'X-Toe3-Reason: manual' \
	--data-binary "@$tmp/snapshot.gz" -o /dev/null || fail "upload failed"
req -f -H "Authorization: Bearer $token" -o "$tmp/latest.gz" "$base/v1/saves/latest" || fail "download failed"
[ "$(sha256sum "$tmp/latest.gz" | cut -d' ' -f1)" = "$sha" ] || fail "downloaded snapshot differs from the upload"

status=$(req -o /dev/null -w '%{http_code}' "$base/v1/saves")
[ "$status" = 401 ] || fail "unauthenticated list returned $status, want 401"
echo "smoke ok: $base"
