#!/bin/sh
set -eu

version=${1:-0.1.0}
case "$version" in
    ''|*[!0-9A-Za-z.+-]*)
        echo 'Invalid release version' >&2
        exit 1
        ;;
esac

mkdir -p dist
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM

for arch in amd64 arm64; do
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
        -ldflags "-s -w -X main.version=$version" \
        -o "$stage/systemd-conf-tui" .
    cp README.md "$stage/README.md"
    cp -R completions "$stage/"
    tar -czf "dist/systemd-conf-tui_${version}_linux_${arch}.tar.gz" \
        -C "$stage" systemd-conf-tui README.md completions
done

(
    cd dist
    sha256sum "systemd-conf-tui_${version}_linux_amd64.tar.gz" \
        "systemd-conf-tui_${version}_linux_arm64.tar.gz" > SHA256SUMS.txt
)
