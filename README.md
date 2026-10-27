# Goshimi relay-go (public mirror)

Public mirror of the `relay-go` forward-proxy binary used by Goshimi relay servers,
plus a one-command installer.

## Install

```bash
curl -fsSL -o /tmp/relay-install.sh https://raw.githubusercontent.com/abbasnazari-0/goshimi-relay-go/main/relay-install.sh
bash /tmp/relay-install.sh          # asks for the port (default 8081)
bash /tmp/relay-install.sh 8443     # or pass the port directly
```

The installer downloads the binary, verifies its SHA256, installs it to
`/opt/relay/relay-go` and creates/enables the `relay` systemd service.
**It requires root** (writes to `/opt` and `/etc/systemd/system`).

## Binary

- Release: `v1`
- Size: 3,036,153 bytes
- SHA256: `5feae7916ec83f83faf98612b7c81227791dcecf39aa3a6b215c4034c2520162`

## Behaviour

`relay-go` is a forward proxy that relays `CONNECT` traffic. It forwards the
client's `Proxy-Authorization` header upstream, so proxy credentials are supplied
by the client (or by the panel entry) — none are baked into the binary or this repo.
