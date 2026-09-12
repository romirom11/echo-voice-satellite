# Releases and updates (this fork)

This fork publishes two independently versioned artifacts, the same way the
controller's update checks expect them (`controller/em_api.py`):

| Artifact | Tag | Where it lands | Who consumes it |
|---|---|---|---|
| Device firmware (`server`, ARMv7) | `vX.Y.Z` | GitHub Release with a `server` asset | Dashboard → device Updates tab (OTA into the inactive A/B slot, automatic rollback) |
| Controller image | `controller-vX.Y.Z` | `ghcr.io/romirom11/echomuse-controller:vX.Y.Z` and `:latest` | Docker deployments (`docker compose pull`), Home Assistant add-on |

The controller polls this repository (`github_repo` in Settings, default
`romirom11/echo-voice-satellite`, seeded from `EM_GITHUB_REPO`) once an hour
and shows a badge when a newer firmware or controller version exists.

## Cutting a firmware release

```bash
git tag -a v2.21.0 -m "What changed, for the person deciding whether to update."
git push origin v2.21.0
```

`.github/workflows/firmware-release.yml` builds the binary with the pinned
NDK image, embeds the tag as the firmware version, verifies it is an ARM
binary and creates the GitHub Release with `server` and `server.sha256`.
The tag annotation is the release note.

## Cutting a controller release

```bash
git tag -a controller-v2.21.0 -m "What changed."
git push origin controller-v2.21.0
```

`.github/workflows/controller-publish.yml` builds the image for amd64 and
arm64 and pushes `:v2.21.0`, `:latest` and `:sha-<short>`. Bump `version:` in
`controller/config.yaml` in the same change so the add-on manifest and the
image agree. Every push to `main` that touches `controller/` also refreshes
`:latest`.

## Updating a Docker deployment

`deploy/` holds a standalone compose file and `update.sh`. Copy both next to
your `data/` directory (e.g. `/opt/echomuse`) and run:

```bash
./update.sh            # newest controller-v* tag on GitHub
./update.sh v2.22.0    # a specific version
./update.sh latest     # what main last published (dev channel)
```

It rewrites the image tag in `docker-compose.yml`, pulls, restarts and prunes
the old image. Device data, users, recordings and TLS material live in
`./data` and survive the swap. The pull happens before the restart, so a
failed download leaves the running controller alone; on a disk too small
for two images it retries with the container stopped.

## Updating devices

Open the device in the dashboard → Updates → **Update**. The controller
downloads the `server` asset once, verifies its md5 on the device, writes it
to the inactive slot and reboots the firmware; a binary that fails to start
three times is rolled back automatically.

## Version strings

- Firmware reports the git tag (`v2.21.0`), or `YYYYMMDD-HHMM-dev` for a
  CI build of an untagged commit.
- The controller reports `EM_CONTROLLER_VERSION` baked into the image
  (`v2.21.0`, or `sha-abc1234` for a `main` build), see `controller/version.py`.
