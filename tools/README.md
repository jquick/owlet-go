# tools

Standalone helpers for getting the credentials the bridge needs. They're pure
Go stdlib (no cgo, no SDK), so they run anywhere — they live in their own
module, separate from the `owletcam` bridge. Run them from this directory.

## extractkey — the SDK license key

`sdk_key` is Owlet's TUTK/Kalay license. It's not distributed here, but it's a
base64 literal (`AQAAA…`) baked into the Owlet app, so you can pull it from your
own copy. Grab the app from any APK mirror and scan it:

```bash
apkeep -a com.owletcare.sleep .        # downloads com.owletcare.sleep.xapk
go run ./extractkey com.owletcare.sleep.xapk
```

It also accepts a single `.apk`, a `.dex`, or a directory of `classes*.dex`.
The longest match (usually the only one) is the key — put it into your `.env`
as `SDK_KEY`.

## capture_auth — the per-camera credentials

`TUTK_UID`, `AUTH_KEY`, and `PASSWORD` are unique to your camera and come from
the app's login/device-lookup traffic. `capture_auth` is a small
HTTPS-intercepting proxy that pulls exactly those three values out of that
traffic (it's the Go replacement for the old mitmproxy addon). It only observes traffic your own phone is already sending; it does
not touch the camera.

```bash
go run ./capture_auth -out ../.env
```

Then, on your phone (same Wi-Fi network):

1. **Set the Wi-Fi HTTP proxy** to this machine's IP on port `8080` (the tool
   prints the exact host/port on startup).
2. **Install the CA:** open `http://owlet.ca` in the phone browser to download
   the profile. On iOS, install it under *Settings → General → VPN & Device
   Management*, then **enable** it under *Settings → General → About →
   Certificate Trust Settings*.
3. **Open the Owlet app,** log in, and open the live camera view.

It stays quiet until it sees the device lookup, then prints each field as it
finds it and a final JSON block — and with `-out` it merges those three values
straight into your `.env` (keeping `SDK_KEY` and anything else intact). If
nothing shows up, it surfaces the reason (e.g. a TLS handshake failure means
the app is pinning its cert).

Flags: `-out` (merge creds into this file), `-addr` (listen address), `-save`
(also dump every full flow to `-dir` for inspection), `-all` (look at every
host, not just Owlet/TUTK), `-v` (log each flow), `-ca-cert`/`-ca-key`.

When you're done, remove the Wi-Fi proxy and delete the trusted CA from the
phone.
