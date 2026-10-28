# Stator chart

Installs Stator: the api, worker, web and render images and Kroki, with
Postgres, Valkey and an S3 store for a trial. Releasing and installing a
release are in [docs/releasing.md](../../../docs/releasing.md); the values are
described in `values.yaml`, and diagrams below.

## Your own images

The chart runs the images from `ghcr.io/praetorianer777/stator-<image>` unless
told otherwise. An image built by your own pipeline should carry the
`VERSION` and `COMMIT` build arguments, from `scripts/build-stamp.sh version`
and `scripts/build-stamp.sh commit`; without `COMMIT` the sidebar says
`commit unknown` and the image build prints a warning. See "Building your own
images" in docs/releasing.md.

## Diagrams

Mermaid diagrams are drawn in the browser. PlantUML (C4 included),
Structurizr, Graphviz and D2 are drawn by Kroki, which the chart runs inside
the release with `kroki.enabled: true`; it is off by default because its
image, `yuzutech/kroki` at the tag `images.kroki` pins, is 4.1 GB. It needs
about 100 MiB at rest and up to 250 MiB and two cores for a moment while
several people draw; `kroki.resources` asks for 256Mi and holds it to 1Gi.

Kroki publishes nothing outside the cluster. Its Service is reached by the
api alone, at the `STATOR_KROKI_URL` the chart sets for it, and a
NetworkPolicy lets in the api's pods
on port 8000 and lets nothing out, not even DNS: no language it draws needs
the network. The policy only holds where the cluster's network plugin
enforces policies, so check that yours does.

Kroki runs with `KROKI_SAFE_MODE=secure`, and the chart has no value to turn
that off. Never run it otherwise, here or beside the chart: in unsafe mode a
diagram's `!include /etc/passwd` or `!include https://...` is carried out,
so anybody who can write a page can read the files of Kroki's container and
make it fetch any address it reaches. In secure mode such lines are dropped
and the rest of the diagram draws.
