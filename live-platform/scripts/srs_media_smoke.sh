#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
SRS_API_URL="${SRS_API_URL:-http://127.0.0.1:1985}"
SRS_CONTAINER="${SRS_CONTAINER:-$(docker ps --filter label=com.docker.compose.service=srs --format '{{.Names}}' | head -n 1)}"
TMP="$(mktemp -d)"
COOKIE="$TMP/cookie"
STAMP="$(date +%s)$$"

cleanup() {
  rm -rf "$TMP"
}
trap cleanup EXIT

json_field() {
  python3 -c "import json,sys; print(json.load(sys.stdin)[$1])"
}

wait_for_media_status() {
  local room_id="$1" expected="$2" status=""
  for _ in $(seq 1 30); do
    status="$(curl -fsS -b "$COOKIE" "$BASE_URL/api/platform/v1/rooms/$room_id/playback" | json_field "'status'" 2>/dev/null || true)"
    [[ "$status" == "$expected" ]] && return 0
    sleep 0.5
  done
  echo "media status: expected $expected, got ${status:-empty}" >&2
  return 1
}

wait_for_recording() {
  local room_id="$1" url=""
  for _ in $(seq 1 40); do
    url="$(curl -fsS -b "$COOKIE" "$BASE_URL/api/platform/v1/rooms/$room_id/recordings" | python3 -c 'import json,sys; items=json.load(sys.stdin).get("items", []); print(items[0].get("playback_url", "") if items else "")' 2>/dev/null || true)"
    if [[ -n "$url" ]]; then
      printf '%s' "$url"
      return 0
    fi
    sleep 0.5
  done
  echo "recording was not published" >&2
  return 1
}

[[ -n "$SRS_CONTAINER" ]] || { echo "SRS container is not running" >&2; exit 1; }
curl -fsS "$BASE_URL" >/dev/null
curl -fsS "$SRS_API_URL/api/v1/versions" >/dev/null

echo "[srs] register anchor and create room"
curl -fsS -c "$COOKIE" -X POST "$BASE_URL/api/platform/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"account\":\"srssmoke$STAMP@example.test\",\"nickname\":\"SRS Smoke\",\"password\":\"password123\"}" >/dev/null
ROOM="$(curl -fsS -b "$COOKIE" -X POST "$BASE_URL/api/platform/v1/rooms" \
  -H 'Content-Type: application/json' -d '{"title":"SRS media smoke"}')"
ROOM_ID="$(printf '%s' "$ROOM" | json_field "'room_id'")"
CREDENTIALS="$(curl -fsS -b "$COOKIE" -X POST "$BASE_URL/api/platform/v1/rooms/$ROOM_ID/media-session")"
STREAM_KEY="$(printf '%s' "$CREDENTIALS" | json_field "'stream_key'")"

echo "[srs] publish synthetic H.264/AAC stream"
docker exec "$SRS_CONTAINER" /usr/local/srs/objs/ffmpeg/bin/ffmpeg \
  -hide_banner -loglevel error -re \
  -f lavfi -i testsrc2=size=640x360:rate=30 \
  -f lavfi -i sine=frequency=1000:sample_rate=48000 \
  -t 20 -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p \
  -g 30 -c:a aac -ar 48000 -f flv "rtmp://127.0.0.1:1935/live/$STREAM_KEY" \
  >"$TMP/ffmpeg.log" 2>&1 &
FFMPEG_PID=$!
wait_for_media_status "$ROOM_ID" PUBLISHING

echo "[srs] start room and negotiate WHEP"
curl -fsS -b "$COOKIE" -X POST "$BASE_URL/api/platform/v1/rooms/$ROOM_ID/start" >/dev/null
PLAYBACK="$(curl -fsS -b "$COOKIE" "$BASE_URL/api/platform/v1/rooms/$ROOM_ID/playback")"
WHEP_URL="$(printf '%s' "$PLAYBACK" | json_field "'whep_url'")"
cat >"$TMP/offer.lf" <<'SDP'
v=0
o=- 0 0 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE 0 1
a=msid-semantic: WMS
m=audio 50000 UDP/TLS/RTP/SAVPF 111
c=IN IP4 127.0.0.1
a=candidate:1 1 UDP 2130706431 127.0.0.1 50000 typ host
a=end-of-candidates
a=ice-ufrag:smoke
a=ice-pwd:smoketestpassword1234567890
a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00
a=setup:actpass
a=mid:0
a=recvonly
a=rtcp-mux
a=rtpmap:111 opus/48000/2
m=video 50000 UDP/TLS/RTP/SAVPF 96
c=IN IP4 127.0.0.1
a=candidate:1 1 UDP 2130706431 127.0.0.1 50000 typ host
a=end-of-candidates
a=ice-ufrag:smoke
a=ice-pwd:smoketestpassword1234567890
a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00
a=setup:actpass
a=mid:1
a=recvonly
a=rtcp-mux
a=rtpmap:96 H264/90000
a=fmtp:96 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f
SDP
sed 's/$/\r/' "$TMP/offer.lf" >"$TMP/offer.sdp"
WHEP_STATUS="$(curl -sS -D "$TMP/whep.headers" -o "$TMP/answer.sdp" -w '%{http_code}' \
  -X POST -H 'Content-Type: application/sdp' --data-binary @"$TMP/offer.sdp" "$WHEP_URL")"
[[ "$WHEP_STATUS" == 201 ]] || { cat "$TMP/answer.sdp" >&2; exit 1; }
grep -q '^v=0' "$TMP/answer.sdp"

RESOURCE="$(awk 'BEGIN{IGNORECASE=1} /^location:/{sub(/\r$/, "", $2); print $2}' "$TMP/whep.headers")"
if [[ -n "$RESOURCE" ]]; then
  RESOURCE="$(python3 -c 'import sys,urllib.parse; print(urllib.parse.urljoin(sys.argv[1], sys.argv[2]))' "$WHEP_URL" "$RESOURCE")"
  curl -fsS -X DELETE "$RESOURCE" >/dev/null
fi

echo "[srs] stop room and verify disconnect callback"
STOPPED="$(curl -fsS -b "$COOKIE" -X POST "$BASE_URL/api/platform/v1/rooms/$ROOM_ID/stop")"
[[ "$(printf '%s' "$STOPPED" | json_field "'status'")" == CLOSED ]]
wait "$FFMPEG_PID"
wait_for_media_status "$ROOM_ID" DISCONNECTED

echo "[srs] verify DVR callback and MP4 replay"
RECORDING_URL="$(wait_for_recording "$ROOM_ID")"
curl -fsS "$RECORDING_URL" -o "$TMP/recording.mp4"
[[ -s "$TMP/recording.mp4" ]]
head -c 64 "$TMP/recording.mp4" | grep -aq 'ftyp'

echo "SRS media smoke: PASS (room=$ROOM_ID, RTMP, callbacks, WHEP, DVR replay, stop)"
