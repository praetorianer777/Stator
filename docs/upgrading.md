# Upgrading

An upgrade is a new image with the migrations it brings. Under the chart the
migrate Job applies them before the api and worker roll (a `pre-upgrade`
hook, or an Argo CD `Sync` hook); under Docker Compose the `migrate` service
does, before the api starts. Either connects as the schema's owner. Goose
runs each migration in a transaction of its own, so a migration that fails
leaves the database as the one before it left it, and the next attempt starts
there.

A failed migrate Job stops the upgrade: the api and worker keep running the
release before. Its log names the migration and the error:

```sh
kubectl logs -n <namespace> job/<release>-stator-migrate
```

Run the upgrade again once the cause is fixed. The Job is deleted and made
anew by each `helm upgrade` (or Argo CD sync), so nothing else needs cleaning
up.

## Troubleshooting

### Under CloudNativePG an upgrade fails in an early migration

Releases before the fix for #423 stop under CloudNativePG when the database
already holds data, most often at `00670_example_renewal.sql`:

```
ERROR 00670_example_renewal.sql: ... ERROR: check constraint "space_example_versioned" of relation "space" is violated by some row (SQLSTATE 23514)
```

and in the same way, with another constraint or a not-null column, at
`00420_attachment_versions.sql` or `00500_live_pages.sql`.

With `cnpg.enabled` the migrations run as the database's owner,
`stator_owner` (or `database.ownerRole`), which CloudNativePG makes without
superuser rights or `BYPASSRLS`. Every table forces row level security on its
owner too, and until migration 00750 the tenant policies showed no row to a
session that named no organization. A migration that fills a new column from
the rows already there then saw none and filled nothing, and the constraint
that follows refused the rows it left empty. A database written by
superuser migrations, as under Docker Compose or `postgresql.enabled`, never
had the problem.

The release with the fix lifts the forced row level security around each such
statement, so upgrading to it is enough. To finish an upgrade to a release
before it, let the owner past row level security for the length of the
upgrade, as CloudNativePG's superuser on the primary:

```sh
NS=<namespace>
CLUSTER=<release>-stator-postgres
PRIMARY=$(kubectl get pods -n "$NS" -l cnpg.io/cluster="$CLUSTER",cnpg.io/instanceRole=primary -o name)

kubectl exec -n "$NS" "$PRIMARY" -c postgres -- psql -c 'ALTER ROLE stator_owner BYPASSRLS'
helm upgrade <release> <chart> -n "$NS" <your usual flags>
kubectl exec -n "$NS" "$PRIMARY" -c postgres -- psql -c 'ALTER ROLE stator_owner NOBYPASSRLS'
```

Take `BYPASSRLS` away again as soon as the migrate Job has succeeded: the
owner is not supposed to reach past the policies, and nothing at run time
needs it to.

An instance installed empty and upgraded through those migrations before it
held data never ran into this and needs nothing. Some migrations did not fail
but quietly skipped their rows, and a migration that went through is never
run again: an instance that already held data under CloudNativePG when it
passed 00070 (first version of every page), 00090 (default grants), 00195
(interface language), 00210 (publication dates) or 00300 (page views)
without the fix is missing what they would have written. 00420 and 00500,
which such an instance reaches next, stop it with the error above.
