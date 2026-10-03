#!/usr/bin/env bash
# Installs golangci-lint from its GitHub release, verifying the archive against
# the published checksums. It replaces the upstream install.sh, whose checksum
# lookup uses an unanchored grep: since v2.13.x the release ships a
# <archive>.sbom.json asset, so the tarball name also matches the SBOM line and
# the script always fails verification. Matching the filename exactly here
# keeps `make tools` working.
set -euo pipefail

usage() {
	echo "usage: $0 --version <vX.Y.Z> [--bindir <dir>]" >&2
	exit 2
}

version=""
bindir=""

while [ "$#" -gt 0 ]; do
	case "$1" in
	--version)
		[ "$#" -ge 2 ] || usage
		version="$2"
		shift 2
		;;
	--bindir)
		[ "$#" -ge 2 ] || usage
		bindir="$2"
		shift 2
		;;
	-h | --help) usage ;;
	*) usage ;;
	esac
done

[ -n "$version" ] || usage

case "$version" in
v*) version="${version#v}" ;;
esac

os="$(go env GOOS)"
arch="$(go env GOARCH)"
name="golangci-lint"
versioned="${name}-${version}"
unpacked="${versioned}-${os}-${arch}"
archive="${unpacked}.tar.gz"
checksums="${versioned}-checksums.txt"
release_url="https://github.com/golangci/${name}/releases/download/v${version}"

sha256() {
	if command -v sha256sum >/dev/null; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "install-golangci-lint: downloading ${archive}"
curl -sSfL -o "$tmp/$archive" "$release_url/$archive"
curl -sSfL -o "$tmp/$checksums" "$release_url/$checksums"

want="$(awk -v file="$archive" '$2 == file { print $1 }' "$tmp/$checksums")"
if [ -z "$want" ]; then
	echo "install-golangci-lint: no checksum published for $archive" >&2
	exit 1
fi

got="$(sha256 "$tmp/$archive")"
if [ "$want" != "$got" ]; then
	echo "install-golangci-lint: checksum mismatch for $archive: $want vs $got" >&2
	exit 1
fi

tar --no-same-owner -xzf "$tmp/$archive" -C "$tmp"

if [ -z "$bindir" ]; then
	bindir="$(go env GOPATH)/bin"
fi
mkdir -p "$bindir"
install -m 0755 "$tmp/$unpacked/$name" "$bindir/$name"

echo "install-golangci-lint: installed $name $version to $bindir"