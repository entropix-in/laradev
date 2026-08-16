#!/bin/sh
set -eu

repo="entropix-in/laradev"
asset="laradev-linux-amd64.tar.gz"
bin_dir="${BIN_DIR:-${HOME}/.local/bin}"
version="${VERSION:-}"
release_root="${RELEASE_BASE_URL:-https://github.com/${repo}/releases}"

die() {
    printf 'laradev installer: %s\n' "$1" >&2
    exit 1
}

case "$(uname -s)" in
    Linux) ;;
    *) die "only Linux is supported" ;;
esac
case "$(uname -m)" in
    x86_64|amd64) ;;
    *) die "only amd64/x86_64 is supported" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1 || die "tar is required"

tmp_dir="$(mktemp -d)"
staged=""
cleanup() {
    rm -rf "$tmp_dir"
    if [ -n "$staged" ]; then
        rm -f "$staged"
    fi
}
trap cleanup 0 1 2 3 15

if [ -n "$version" ]; then
    release_base="${release_root}/download/${version}"
else
    release_base="${release_root}/latest/download"
fi

archive_path="${tmp_dir}/${asset}"
checksum_path="${tmp_dir}/${asset}.sha256"
curl --fail --silent --show-error --location "${release_base}/${asset}" --output "$archive_path"
curl --fail --silent --show-error --location "${release_base}/${asset}.sha256" --output "$checksum_path"

if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp_dir" && sha256sum --check "${asset}.sha256")
elif command -v shasum >/dev/null 2>&1; then
    (cd "$tmp_dir" && shasum --algorithm 256 --check "${asset}.sha256")
else
    die "sha256sum or shasum is required"
fi

tar --extract --gzip --file "$archive_path" --directory "$tmp_dir" --no-same-owner --no-same-permissions laradev
[ -x "${tmp_dir}/laradev" ] || die "release archive did not contain an executable laradev binary"

mkdir -p "$bin_dir"
target="${bin_dir}/laradev"
manifest="${bin_dir}/.laradev-install.json"
if [ -e "$target" ] && [ ! -e "$manifest" ]; then
    die "refusing to replace ${target} without a laradev install manifest"
fi

staged="$(mktemp "${bin_dir}/.laradev-installer.XXXXXX")"
install -m 0755 "${tmp_dir}/laradev" "$staged"
mv -- "$staged" "$target"
"$target" install --bin-dir "$bin_dir"
printf 'laradev installed in %s\n' "$target"
