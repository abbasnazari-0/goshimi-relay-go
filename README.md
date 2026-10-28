# Goshimi relay-go (public mirror)

Public mirror of the `relay-go` forward-proxy binary used by Goshimi relay servers,
plus a one-command installer that also adds the status/health front.

## Install

```bash
curl -fsSL -o /tmp/install.sh https://raw.githubusercontent.com/abbasnazari-0/goshimi-relay-go/main/install.sh
bash /tmp/install.sh          # asks for the public port (default 8081)
bash /tmp/install.sh 8443     # or pass it directly
HEALTH_PORT=9090 bash /tmp/install.sh 8443
```

The installer requires **root** (writes to `/opt` and `/etc/systemd/system`) and sets up two units:

| unit | listens | purpose |
|---|---|---|
| `relay.service` | `127.0.0.1:18082` (internal) | `relay-go` forward proxy |
| `relay-health.service` | `:8081` and `:80` | `relay-health` front |

`relay-health` answers a plain browser request exactly like the Shifter relay:

```
$ curl -i http://IP:8081/
HTTP/1.1 200 OK
Content-Type: application/json
Content-Length: 16
Connection: close

{"status":"ok"}
```

while every real proxy request (`CONNECT host:443`, or an absolute-URI request) is
piped byte-for-byte to `relay-go`. Port `18082` is blocked from outside with an
`iptables` rule (best effort).

## Binaries

| asset | size | sha256 |
|---|---|---|
| `relay-go` (v2) | 3,125,569 | `8c1fe78df25aa01d5fb525f355c4c134e7e7ef606284a46861be2716db34932d` |
| `relay-go-arm64` (v2) | 3,022,071 | `75543c320389b2489cc5d39cb28528b731a6c1780fa569e6d6d0976e479cfba9` |
| `relay-health` | 2,314,400 | `57b7cdb09d530f1354e8f1ddade47f6c5bbf2b3f1610eb6874d38b9d4962e008` |

## v2 changes (2026-10-28)

- Warm pool per (auth, target): 3 → **10** tunnels, TTL 30s → **60s**
- Client idle timeout 15s → **60s** (fewer re-CONNECTs for keep-alive clients)
- TCP keep-alive 10s → 30s
- Two extra pre-warmed AdMob hosts: `adservice.google.com`, `securepubads.g.doubleclick.net`
- `arm64` binary published alongside `amd64`
- Source included (`relay.go`)



## Credentials

`relay-go` forwards the client's `Proxy-Authorization` header upstream, so proxy
credentials are supplied by the client (or by the panel entry) — none are baked
into the binaries or this repo.

