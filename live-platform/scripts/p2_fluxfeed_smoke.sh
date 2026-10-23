#!/usr/bin/env bash
set -euo pipefail

api_base="${LUMIFLOW_API_ORIGIN:-http://127.0.0.1:18080}"
suffix="$(date +%s%N)"
account="p2-${suffix}@lumiflow.test"
password="P2Password123!"
idem="p2-video-${suffix}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

require_status() {
  local expected="$1" actual="$2" body="$3"
  if [[ "$actual" != "$expected" ]]; then
    printf 'unexpected HTTP status: got %s, want %s\n' "$actual" "$expected" >&2
    cat "$body" >&2
    exit 1
  fi
}

curl -fsS "$api_base/health" >/dev/null

register_body="$tmpdir/register.json"
register_status="$(curl -sS -o "$register_body" -w '%{http_code}' -X POST "$api_base/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  --data "{\"account\":\"$account\",\"password\":\"$password\",\"nickname\":\"P2 smoke\"}")"
require_status 201 "$register_status" "$register_body"
token="$(jq -er '.access_token' "$register_body")"

curl -fsS "$api_base/api/v1/me" -H "Authorization: Bearer $token" >/dev/null

# Tiny valid H.264 MP4 and PNG fixtures keep this smoke test independent of
# host tools. FluxFeed still runs ffprobe/ffmpeg against the uploaded video.
base64 --decode >"$tmpdir/video.mp4" <<'EOF'
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
EOF
base64 --decode >"$tmpdir/cover.png" <<'EOF'
iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL1LwAAAABJRU5ErkJggg==
EOF

upload_asset() {
  local kind="$1" file="$2" body="$3"
  local status
  status="$(curl -sS -o "$body" -w '%{http_code}' -X POST "$api_base/api/v1/content-assets" \
    -H "Authorization: Bearer $token" \
    -F "kind=$kind" \
    -F "file=@$file")"
  require_status 201 "$status" "$body"
  jq -er '.url' "$body"
}

media_url="$(upload_asset video "$tmpdir/video.mp4" "$tmpdir/video-upload.json")"
cover_url="$(upload_asset cover "$tmpdir/cover.png" "$tmpdir/cover-upload.json")"

video_body="$tmpdir/video.json"
create_video() {
  curl -sS -o "$video_body" -w '%{http_code}' -X POST "$api_base/api/v1/videos" \
    -H "Authorization: Bearer $token" \
    -H "Idempotency-Key: $idem" \
    -H 'Content-Type: application/json' \
    --data "$(jq -n --arg media_url "$media_url" --arg cover_url "$cover_url" '{title:"P2 smoke video",description:"gateway to FluxFeed",media_url:$media_url,cover_url:$cover_url}')"
}

require_status 202 "$(create_video)" "$video_body"
video_id="$(jq -er '.id' "$video_body")"
require_status 200 "$(create_video)" "$video_body"

curl -fsS "$api_base/api/v1/users/me/videos" -H "Authorization: Bearer $token" | jq -e --argjson id "$video_id" '.items[] | select(.id == $id)' >/dev/null
if [[ "${P2_KEEP_VIDEO:-false}" != "true" ]]; then
  require_status 204 "$(curl -sS -o "$tmpdir/delete.json" -w '%{http_code}' -X DELETE "$api_base/api/v1/videos/$video_id" -H "Authorization: Bearer $token")" "$tmpdir/delete.json"
fi
curl -fsS "$api_base/api/v1/feed-items" >/dev/null

printf 'P2 FluxFeed smoke passed: registered user, projected LUMI identity, uploaded assets, created/replayed/listed video; video_id=%s keep_video=%s.\n' "$video_id" "${P2_KEEP_VIDEO:-false}"
