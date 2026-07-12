# Build the Go bridge (cgo -> stock Kalay SDK) and run it. Build context = this dir.

FROM golang:1.23-bookworm AS build
ARG TARGETARCH
# Pin to a known-good docker-wyze-bridge commit for the stock (glibc) Kalay SDK.
ARG WYZE_REF=main
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Stock ThroughTek Kalay SDK (generic glibc build; the same lib Wyze ships).
RUN set -eux; \
    case "$TARGETARCH" in \
      amd64) LIB=amd64 ;; arm64) LIB=arm64 ;; arm) LIB=arm ;; *) LIB=amd64 ;; \
    esac; \
    curl -fSL "https://raw.githubusercontent.com/mrlt8/docker-wyze-bridge/${WYZE_REF}/app/lib/lib.${LIB}" \
      -o /usr/local/lib/libIOTCAPIs_ALL.so; \
    ldconfig

# LD_PRELOAD shims (network steering only; see entrypoint.sh).
COPY shims/ /tmp/shims/
RUN gcc -shared -fPIC -o /nomaster.so /tmp/shims/nomaster.c -ldl

WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY assets ./assets
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -o /owletcam ./cmd

# --- runtime --------------------------------------------------------------- #
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libstdc++6 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /usr/local/lib/libIOTCAPIs_ALL.so /usr/local/lib/
COPY --from=build /nomaster.so /usr/local/lib/
RUN ldconfig
COPY --from=build /owletcam /usr/local/bin/owletcam
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENV HTTP_PORT=8091 \
    QUALITY=high \
    LAN_ONLY=1
EXPOSE 8091
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
