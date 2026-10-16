# Stator chart

Installs Stator: the api, worker, web and render images, with Postgres,
Valkey and an S3 store for a trial. Releasing and installing a release are in
[docs/releasing.md](../../../docs/releasing.md); the values are described in
`values.yaml`.

## Your own images

The chart runs the images from `ghcr.io/praetorianer777/stator-<image>` unless
told otherwise. An image built by your own pipeline should carry the
`VERSION` and `COMMIT` build arguments, from `scripts/build-stamp.sh version`
and `scripts/build-stamp.sh commit`; without `COMMIT` the sidebar says
`commit unknown` and the image build prints a warning. See "Building your own
images" in docs/releasing.md.
