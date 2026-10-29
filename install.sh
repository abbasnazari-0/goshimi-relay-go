#!/usr/bin/env bash
# Goshimi relay installer
#   relay-go      -> forward proxy (internal, 127.0.0.1:18082)
#   relay-health  -> public front: plain browser request returns {"status":"ok"}
#                    while CONNECT / proxy traffic is piped to relay-go
#
# Usage:
#   bash install.sh            # asks for the public port (default 8081)
#   bash install.sh 8443       # public port 8443, health also on :80
#   HEALTH_PORT=9090 bash install.sh 8443
set -euo pipefail

REPO="abbasnazari-0/goshimi-relay-go"
REL="https://github.com/$REPO/releases/download/v2.1"

# v2.1 binaries: warm pool rejects tunnels older than 20s (stale residential
# exits caused SSLHandshake errors on reuse) and TTL reduced to 25s.
ARCH="$(uname -m)"
if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
  BIN_URL="$REL/relay-go-arm64"
  BIN_SHA="4674451d511aae7d3c0e6eaf5cfa4a34f73402fee2e1dce9d3e958d2e821d0f8"
else
  BIN_URL="$REL/relay-go"
  BIN_SHA="a1c22374e54bf7c64c1eaa0ed5349f05d0f8c36814f076f0e646c34d08b2cb74"
fi
HLT_URL="$REL/relay-health"
HLT_SHA="57b7cdb09d530f1354e8f1ddade47f6c5bbf2b3f1610eb6874d38b9d4962e008"

DEST="/opt/relay"
UNIT_DIR="/etc/systemd/system"
INTERNAL_PORT="${INTERNAL_PORT:-18082}"
HEALTH_PORT="${HEALTH_PORT:-80}"

PORT="${1:-}"
if [ -z "$PORT" ]; then
  read -r -p "پورت عمومی رله [پیش‌فرض 8081]: " PORT
  PORT="${PORT:-8081}"
fi
if ! [[ "$PORT" =~ ^[0-9]+$ ]] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
  echo "پورت نامعتبر: $PORT" >&2
  exit 1
fi
if [ "$PORT" = "$INTERNAL_PORT" ]; then
  echo "پورت $PORT با پورت داخلی ($INTERNAL_PORT) یکی است؛ عدد دیگری بده." >&2
  exit 1
fi

echo "→ دانلود باینری‌ها از GitHub ..."
sudo mkdir -p "$DEST"

fetch() { # url expected_sha tmpfile label
  local url="$1" sha="$2" tmp="$3" label="$4"
  if ! sudo curl -fsSL --max-time 180 -o "$tmp" "$url"; then
    echo "   تلاش دوباره با دور زدن کش CDN ..."
    sudo curl -fsSL --max-time 180 -o "$tmp" "$url?cb=$(date +%s)"
  fi
  local got
  got="$(sha256sum "$tmp" | awk '{print $1}')"
  if [ "$got" != "$sha" ]; then
    echo "SHA256 نادرست برای $label: $got" >&2
    exit 1
  fi
  echo "   $label  ✓"
}

fetch "$BIN_URL" "$BIN_SHA" /tmp/relay-go "relay-go"
fetch "$HLT_URL" "$HLT_SHA" /tmp/relay-health "relay-health"

echo "→ نصب ..."
sudo install -m 755 /tmp/relay-go "$DEST/relay-go"
sudo install -m 755 /tmp/relay-health "$DEST/relay-health"
rm -f /tmp/relay-go /tmp/relay-health

echo "→ سرویس relay (پروکسی داخلی روی $INTERNAL_PORT) ..."
sudo tee "$UNIT_DIR/relay.service" >/dev/null <<EOF
[Unit]
Description=Relay forward proxy (internal)
After=network.target

[Service]
ExecStart=$DEST/relay-go
Environment=PORT=$INTERNAL_PORT
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

echo "→ سرویس relay-health (پورت $PORT و :$HEALTH_PORT هر دو جواب می‌دهند) ..."
sudo tee "$UNIT_DIR/relay-health.service" >/dev/null <<EOF
[Unit]
Description=Relay health/status front
After=network.target relay.service
Wants=relay.service

[Service]
ExecStart=$DEST/relay-health
Environment=LISTEN_PROXY=:$PORT
Environment=LISTEN_HEALTH=:$HEALTH_PORT
Environment=UPSTREAM=127.0.0.1:$INTERNAL_PORT
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

echo "→ بستن پورت داخلی $INTERNAL_PORT از بیرون ..."
if command -v iptables >/dev/null 2>&1; then
  sudo iptables -C INPUT -p tcp --dport "$INTERNAL_PORT" ! -i lo -j DROP 2>/dev/null \
    || sudo iptables -I INPUT -p tcp --dport "$INTERNAL_PORT" ! -i lo -j DROP 2>/dev/null \
    || echo "   (iptables در دسترس نبود — مطمئن شو پورت $INTERNAL_PORT در فایروال بسته است)"
fi

sudo systemctl daemon-reload
sudo systemctl enable relay >/dev/null 2>&1 || true
sudo systemctl enable relay-health >/dev/null 2>&1 || true
sudo systemctl restart relay
sleep 1
sudo systemctl restart relay-health
sleep 2

echo
sudo systemctl --no-pager --full status relay | head -6 || true
echo
sudo systemctl --no-pager --full status relay-health | head -8 || true

echo
echo "✓ نصب شد."
echo "  • پروکسی:  http://IP:$PORT   (اعتبار Shifter را کلاینت می‌فرستد)"
echo "  • سلامت:   http://IP:$PORT/  و  http://IP:$HEALTH_PORT/  →  {\"status\":\"ok\"}"
echo
echo "تست روی همین سرور:"
echo "  curl -s http://127.0.0.1:$PORT/ ; echo"
echo "  curl -s http://127.0.0.1:$HEALTH_PORT/ ; echo"
echo "  curl -x http://127.0.0.1:$PORT -U '<PROXY_USER>:<PROXY_PASS>' https://api.ipify.org"
echo
echo "برای ثبت در پنل (سرورهای رله → افزودن):"
PUBIP="$(curl -s --max-time 5 https://api.ipify.org 2>/dev/null || true)"
[ -z "$PUBIP" ] && PUBIP="$(hostname -I 2>/dev/null | awk '{print $1}')"
echo "  آدرس: $PUBIP"
echo "  پورت: $PORT"
echo "  یوزرنیم/پسورد پروکسی: همان اعتبار Shifter (از پنل کپی کن)"
echo
echo "یادت نره پورت‌های $PORT و $HEALTH_PORT را در فایروال/GCP باز کنی."
