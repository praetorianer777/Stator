# Backup and restore

One command writes a site's database and every stored file into one archive,
and one command brings a site back from it. It works the same way under
Docker Compose and under the Helm chart: the `backup` image (built from
`deploy/Dockerfile.backend` with `--target backup`) carries the command beside
`pg_dump` and `psql` of the server's major version.

## What an archive holds

`stator-<time>.tar`, with the time in UTC, holds:

- `manifest.json`: the Stator version and schema migrations it was taken with,
  when, and a fingerprint of `STATOR_SECRET_KEY` (not the key);
- `database.sql.gz`: a `pg_dump` of the whole database, with its grants and
  row level security policies;
- `objects/...`: every object in the bucket, under its key: attachments,
  previews, theme assets, space exports and imports.

It does **not** hold:

- **`STATOR_SECRET_KEY`.** Sign-in providers' client secrets and Armature
  tokens are stored sealed with it. Keep the key apart from the archives; a
  restore refuses to run under any other key.
- **The database roles and their passwords**, which belong to the server, not
  to the database. The restoring site must have the same role names.
- **Your identity provider**, Valkey (it holds nothing that outlives a
  restart) and the deployment's own settings.

## Docker Compose

```sh
make backup                        # writes backups/stator-<time>.tar
make restore                       # restores the newest archive in backups/
make restore FROM=stator-20261011T190000Z.tar
```

`STATOR_BACKUP_DIR` names another directory for the archives. `make restore`
stops the api and the worker, restores, and starts them again whether it
worked or not. Without make:

```sh
docker compose run --rm backup create
docker compose stop api worker
docker compose run --rm backup restore -yes [-from /backups/<archive>]
docker compose start api worker
```

## Kubernetes

Set `backup.enabled` and the chart renders a volume for the archives
(`backup.size`, or `backup.existingClaim` for one of yours) and two
CronJobs that never run on their own. Start one with `kubectl`:

```sh
kubectl create job --from=cronjob/<release>-stator-backup backup-$(date +%s)
```

To restore, scale the api and the worker to zero, run the restore job, and
scale them back:

```sh
kubectl scale deployment <release>-stator-api <release>-stator-worker --replicas=0
kubectl create job --from=cronjob/<release>-stator-restore restore-$(date +%s)
kubectl wait --for=condition=complete --timeout=1h job/restore-<time>
kubectl scale deployment <release>-stator-api --replicas=<api.replicas>
kubectl scale deployment <release>-stator-worker --replicas=<worker.replicas>
```

The restore reads the newest archive on the volume, or the one
`backup.restoreFrom` names. To take backups on a schedule, set
`backup.suspend` to false; `backup.schedule` says when. Stator never removes
old archives, and a volume in the same cluster is no protection against
losing the cluster: copy the archives off it.

With CloudNativePG you can also have the operator back the database up
continuously to object storage and recover a new cluster to a point in time;
set it under `cnpg.spec`, and recover with a `bootstrap.recovery`, which the
chart leaves alone. That covers the database only: the bucket still needs the
backup job, or your storage's own versioning and replication.

## How a backup is taken

The site keeps running. `pg_dump` reads one consistent snapshot of the
database first, and the bucket is copied after it, so every file a backed-up
row names is copied as well. An object removed between the two, such as by
the worker emptying the trash, is left out and named in the output.

## How a restore works, and what it refuses

A restore replaces the site: its database and its bucket end as the archive's,
and objects the archive does not hold are removed from the bucket. Before it
changes anything, it refuses:

- without `-yes`;
- an archive taken with a newer Stator, whose schema this one does not know;
  restore it with that version or later;
- under a `STATOR_SECRET_KEY` other than the one the backed-up site ran with;
- while anybody but a superuser is connected to the database, which means the
  api or the worker is still running.

The database is replaced in one transaction, so a restore that fails there
leaves the site as it was. The bucket is restored after it; if that fails
part way, run the restore again. Last, the database is migrated to this
version's schema, so an archive from an older Stator comes back up to date.

### An archive that holds several organizations

An instance serves one organization (#380), and the migration that brings
this in refuses a database holding several. An archive taken before it, of
an instance that held several, restores, but its last step fails with the
migration's message naming them: the database and the bucket are the
archive's, at the schema it was taken with. Run the release the archive was
taken with against it, split it as `docs/organization-transfer.md` says
(export every organization, delete all but one), then upgrade.

## Checking a restore

`make test-backup` is the check CI runs on every change: it backs up the
running stack, restores the archive into a database and a bucket of their
own, backs those up again and compares the two archives, then makes sure each
refusal above changes nothing. After a real restore, sign in, open a page with
an attachment, and look at its history.
