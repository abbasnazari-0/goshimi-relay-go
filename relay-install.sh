#!/usr/bin/env bash
# Goshimi relay-go installer — downloads the binary, installs it and creates
# a systemd service. Usage:
#   bash relay-install.sh            # asks for the port
#   bash relay-install.sh 8443       # uses 8443 without asking
set -euo pipefail

BIN_URL="https://github.com/abbasnazari-0/goshimi-relay-go/releases/download/v1/relay-go"
BIN_SHA="5feae7916ec83f83faf98612b7c81227791dcecf39aa3a6b215c4034c2520162"
DEST="/opt/relay/relay-go"
SERVICE="/etc/systemd/system/relay.service"

PORT="${1:-}"
if [ -z "$PORT" ]; then
  read -r -p "پورت رله [پیش‌فرض 8081]: " PORT
  PORT="${PORT:-8081}"
fi
if ! [[ "$PORT" =~ ^[0-9]+$ ]] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
  echo "پورت نامعتبر: $PORT" >&2
  exit 1
fi

echo "→ دانلود باینری از GitHub ..."
sudo mkdir -p /opt/relay
sudo curl -fL --max-time 180 -o /tmp/relay-go "$BIN_URL"

echo "→ بررسی SHA256 ..."
ACTUAL_SHA="$(sha256sum /tmp/relay-go | awk '{print $1}')"
if [ "$ACTUAL_SHA" != "$BIN_SHA" ]; then
  echo "SHA256 نادرست است: $ACTUAL_SHA" >&2
  exit 1
fi

echo "→ نصب باینری ..."
sudo install -m 755 /tmp/relay-go "$DEST"
rm -f /tmp/relay-go

echo "→ ساخت سرویس systemd ..."
sudo tee "$SERVICE" >/dev/null <<EOF
[Unit]
Description=Relay forward proxy
After=network.target

[Service]
ExecStart=$DEST
Environment=PORT=$PORT
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable relay >/dev/null 2>&1 || true
sudo systemctl restart relay
sleep 2

echo
sudo systemctl --no-pager --full status relay | head -12 || true

echo
echo "✓ رله روی پورت $PORT نصب و اجرا شد."
echo
echo "تست محلی (اعتبار Shifter را از پنل → سرورهای رله بردار):"
echo "  curl -x http://127.0.0.1:$PORT -U '<PROXY_USER>:<PROXY_PASS>' https://api.ipify.org"
echo
echo "برای ثبت در پنل (سرورهای رله → افزودن):"
PUBIP="$(curl -s --max-time 5 https://api.ipify.org 2>/dev/null || true)"
[ -z "$PUBIP" ] && PUBIP="$(hostname -I 2>/dev/null | awk '{print $1}')"
echo "  آدرس: $PUBIP"
echo "  پورت: $PORT"
echo "  یوزرنیم/پسورد پروکسی: همان اعتبار Shifter (از پنل کپی کن)"
echo
echo "یادت نره پورت $PORT را در فایروال/GCP باز کنی."
