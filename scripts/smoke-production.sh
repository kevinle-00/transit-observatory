#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <web-url> <api-url>" >&2
  exit 2
fi

for command in curl jq; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 2; }
done

web_url="${1%/}"
api_url="${2%/}"
if [[ ! "$web_url" =~ ^https?:// || ! "$api_url" =~ ^https?:// ]]; then
  echo "web and API URLs must use HTTP or HTTPS" >&2
  exit 2
fi

curl_args=(--fail --silent --show-error --location --retry 3 --retry-all-errors --connect-timeout 10 --max-time 30)
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

curl "${curl_args[@]}" "$api_url/live" | jq -e '.status == "ok"' >/dev/null
curl "${curl_args[@]}" "$api_url/health" | jq -e '.status == "ok"' >/dev/null
curl "${curl_args[@]}" "$api_url/api/v1/status" >"$temporary_directory/status.json"
jq -e '.data.overall_status | IN("ok", "degraded", "unavailable")' "$temporary_directory/status.json" >/dev/null
jq -e '.data.service_alerts.freshness and .data.static_gtfs.freshness' "$temporary_directory/status.json" >/dev/null

for path in / /lines /stations /alerts /analytics; do
  curl "${curl_args[@]}" "$web_url$path" | grep -Fq '<div id="root"></div>'
done
curl "${curl_args[@]}" "$web_url/health" >/dev/null

curl "${curl_args[@]}" -D "$temporary_directory/cors.headers" -o /dev/null -H "Origin: $web_url" "$api_url/health"
tr -d '\r' <"$temporary_directory/cors.headers" | grep -Fiqx "Access-Control-Allow-Origin: $web_url"

if [[ "${SMOKE_REQUIRE_DATA:-false}" == "true" ]]; then
  jq -e '.data.overall_status != "unavailable"' "$temporary_directory/status.json" >/dev/null
  jq -e '.data.service_alerts.last_applied.archive.object_key | length > 0' "$temporary_directory/status.json" >/dev/null
  jq -e '.data.static_gtfs.last_applied.archive.object_key | length > 0' "$temporary_directory/status.json" >/dev/null
fi

echo "production smoke checks passed"
