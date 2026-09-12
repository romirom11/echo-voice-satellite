#!/bin/bash
# Update the EchoMuse controller running from docker-compose.yml in this
# directory. See docs/releases.md.
#
#   ./update.sh            # newest controller-v* release on GitHub
#   ./update.sh v2.22.0    # a specific version
#   ./update.sh latest     # whatever main last published (dev channel)
#
# ./data (database, recordings, TLS material) is untouched by the image swap.
set -euo pipefail
cd "$(dirname "$0")"

repo="${EM_GITHUB_REPO:-romirom11/echo-voice-satellite}"
image="ghcr.io/${repo%%/*}/echomuse-controller"

want="${1:-}"
if [ -z "$want" ]; then
  # Controller releases are controller-v* tags (the image is the artifact,
  # there is no GitHub Release object) — same lookup the dashboard does.
  want="$(curl -fsSL "https://api.github.com/repos/${repo}/git/matching-refs/tags/controller-v" \
    | grep -oE '"ref": *"refs/tags/controller-v[^"]+"' \
    | sed -E 's#.*controller-(v[^"]+)"#\1#' | sort -V | tail -1)"
  [ -n "$want" ] || { echo "no controller-v* tag found in ${repo}" >&2; exit 1; }
fi

current="$(sed -nE 's#^\s*image:\s*'"${image}"':(\S+).*#\1#p' docker-compose.yml)"
echo "controller image: ${image}"
echo "  running: ${current:-?}"
echo "  wanted:  ${want}"
sed -i -E 's#^(\s*image:\s*'"${image}"':)\S+#\1'"${want}"'#' docker-compose.yml

# Pull first so the running controller keeps serving if the download fails.
# On a small disk two full images may not fit; then do the swap with the
# container stopped so the old image can be removed before the new one lands.
if ! docker compose pull; then
  echo "pull failed (usually disk space) — retrying with the controller stopped"
  docker compose down
  docker image prune -af >/dev/null
  docker compose pull
fi
docker compose up -d
docker image prune -f >/dev/null
docker compose ps
