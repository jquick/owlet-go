#!/bin/sh
# LAN-only by default: block cloud master lookups via an LD_PRELOAD shim that
# only steers the SDK's network I/O; it doesn't touch the protocol.
set -e

if [ "${LAN_ONLY:-1}" != "0" ]; then
    export LD_PRELOAD="/usr/local/lib/nomaster.so${LD_PRELOAD:+:$LD_PRELOAD}"
    echo "[entrypoint] LAN-only mode: master lookups blocked" >&2
fi

exec /usr/local/bin/owletcam
