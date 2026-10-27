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
| `relay-go` | 3,036,153 | `5feae7916ec83f83faf98612b7c81227791dcecf39aa3a6b215c4034c2520162` |
| `relay-health` | 2,314,400 | `57b7cdb09d530f1354e8f1ddade47f6c5bbf2b3f1610eb6874d38b9d4962e008` |

## Credentials

`relay-go` forwards the client's `Proxy-Authorization` header upstream, so proxy
credentials are supplied by the client (or by the panel entry) — none are baked
into the binaries or this repo.

