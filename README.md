# owlet-go

A clean, single-binary Go rewrite of the Owlet Cam bridge. It connects to the
camera over Kalay P2P (via the stock ThroughTek SDK through cgo), pulls the raw
H.264 + AAC FIFOs, and serves a browser player that decodes them with WebCodecs.
No ffmpeg, no muxing, no re-encoding.

## What you need

Config lives in a `.env` file here in `owlet-go/`. Start from the template:

```bash
cp .env.sample .env
```

Then fill in two things:

**1. The SDK license key** (`SDK_KEY`) — Owlet's Kalay license, baked into the
app and the same for every Owlet cam. Extract it from your own copy of the app
once (see [`tools/`](tools)):

```bash
cd tools
apkeep -a com.owletcare.sleep .                 # downloads the .xapk
go run ./extractkey com.owletcare.sleep.xapk    # prints the AQAAA… key
```

**2. Your camera's credentials** (`TUTK_UID`, `AUTH_KEY`, `PASSWORD`) — unique
to your camera, captured from the app's login traffic:

```bash
cd tools
go run ./capture_auth -out ../.env
```

Then set your phone's Wi-Fi proxy to this machine, install the CA from
`http://owlet.ca`, and open the Owlet app. The tool writes the three values into
`.env` for you (leaving `SDK_KEY` intact). See
[`tools/README.md`](tools/README.md) for the full walkthrough.

The finished `.env` looks like:

```dotenv
SDK_KEY=AQAAA…
TUTK_UID=…
AUTH_KEY=…
PASSWORD=…
```

(`capture_auth -out` fills in `TUTK_UID`/`AUTH_KEY`/`PASSWORD`; paste in the
`SDK_KEY` from step 1.)

> The bridge must run on a host that shares the **camera's LAN** — a Linux box
> using Docker host networking. Docker Desktop on Mac/Windows **can't** reach the
> camera: its VM is NAT'd off your LAN, so it can't even ping the camera's IP.
>
> **On macOS**, run Docker under [Colima](https://github.com/abiosoft/colima)
> with **bridged** networking so the VM gets its own IP directly on your LAN and
> can hit the camera. Plain Docker Desktop (or Colima's default NAT mode) will
> not work.

## Run (Docker)

```bash
docker compose up -d --build
docker compose logs -f          # expect "A/V channel authenticated" + fps
```

The server speaks **HTTPS** with a self-signed cert by default (WebCodecs needs a
secure context), so just open `https://<host>:8091/` and accept the browser
warning once — no proxy or tunnel needed. Set `TLS=0` for plain HTTP, or point
`TLS_CERT`/`TLS_KEY` at a real PEM pair. The player connects its WebSocket
relative to its own path, so it also works behind a reverse proxy.

## Config (`.env`)

All config comes from the environment. For local runs the app loads `.env` (or
`../.env`) as a base; real env vars always override it. Under an orchestrator
like Portainer you can skip the file entirely and just set the four secret vars
(`TUTK_UID`, `AUTH_KEY`, `PASSWORD`, `SDK_KEY`) in its environment editor — the
compose file passes them straight through.

| var | required | default | notes |
|-----|:--------:|---------|-------|
| `TUTK_UID` | ✓ | — | camera P2P UID |
| `AUTH_KEY` | ✓ | — | per-camera auth key |
| `PASSWORD` | ✓ | — | per-camera A/V password |
| `SDK_KEY` | ✓ | — | Owlet Kalay license key |
| `HTTP_PORT` | | `8091` | player + WebSocket |
| `TLS` | | `1` | `1` self-signed HTTPS, `0` plain HTTP (`TLS_CERT`/`TLS_KEY` for a real cert) |
| `QUALITY` | | `high` | `max\|high\|hd\|middle\|sd\|low\|ld` |
| `AUDIO` | | `1` | `0` to disable audio |
| `IDLE_TIMEOUT` | | `600` | seconds to keep the camera connected after the last viewer leaves (`0` = disconnect immediately) |
| `LAN_ONLY` | | `1` | `0` allows the cloud rendezvous |
