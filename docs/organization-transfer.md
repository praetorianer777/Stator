# Moving an organization

An administrator of an organization exports all of it as one archive, and a
fresh Stator takes that archive in as its organization. It is how an
organization moves to an instance of its own, and how an instance that holds
several organizations is split before it upgrades to one organization per
instance (#380). A backup (`docs/backup-and-restore.md`) is the other way to
carry a site: it holds the whole database and bucket, every organization in
it, and comes back only as the same site.

Both run in the worker, which a page in the settings follows: Settings, then
**Export and import** (`/settings/transfer`).

## Splitting an instance before the upgrade

An instance serves one organization (#380). The upgrade that brings this in
refuses an instance holding several, and its `migrate` step (the compose
service, or the chart's migrate Job) fails with a message naming them:

```text
This instance holds 2 organizations (acme, globex), and Stator now serves one
organization per instance. Export every organization (Settings, Export and
import; see docs/organization-transfer.md), delete all but one, then upgrade
again.
```

The refused migration changes nothing, so the database is as it was before
the upgrade. Go back to the release you ran before, and then:

1. Take a backup (`docs/backup-and-restore.md`), in case a step below goes
   wrong.
2. In every organization, an administrator exports it under Settings,
   Export and import, and keeps the archive.
3. Choose the organization that stays on this instance. Delete each of the
   others as the database's owner (`stator` on the compose stack), naming it
   by its slug, and note its id first: its files are under `org/<id>/` in the
   bucket.

   ```sql
   SELECT id FROM org WHERE slug = 'globex';
   BEGIN;
   DELETE FROM user_session WHERE proof_org_id = (SELECT id FROM org WHERE slug = 'globex');
   -- Cleared first: the organization's own theme and hub page go with it.
   UPDATE org SET default_theme_id = NULL, hub_page_id = NULL WHERE slug = 'globex';
   DELETE FROM org WHERE slug = 'globex';
   COMMIT;
   ```

   Every row of the organization goes with it. The worker removes the files
   of its pages, which the deletion leaves tombstones for; remove what is
   left under `org/<id>/` in the bucket yourself, such as its themes' files
   and its logo.
4. Upgrade again. With one organization left, the migrations go through and
   it keeps everything it holds.
5. For each archive of an organization you deleted, install a fresh Stator
   of the new release, bootstrap it (`seed bootstrap`, or the chart's
   bootstrap Job) and import the archive there, as below.

## Exporting

**Export organization** queues the export. The worker writes the archive in
the background, and the page offers it for download for
`STATOR_SPACE_EXPORT_TTL` (a day by default), as it does a space's export.
Only the organization's administrators queue an export, list them or
download one; the database holds the `site_export` rows to them, and the
worker checks again that whoever asked is still one before it begins.

`<slug>-organization-<date>.zip` holds:

- `manifest.json`: format `stator.organization`, version 1; the
  organization's name and settings (anonymous reading, public links, the
  hub page and whether everybody lands on it, the default theme); its people
  with their roles, a guest with the space they were invited to; its groups
  with their members; its global permissions; the sign-in provider and the
  roles its groups give, without the client secret; the Armature connection,
  without its webhook secret; the brand's footer lines; its themes; the
  templates every space offers; its webhooks, without their secrets; each
  person's notification settings and chosen theme; the list of its spaces;
  and `notCarried`, the list below, with counts where they help.
- `spaces/<KEY>.zip`: each space as a space archive (`docs/decisions.md`,
  2026-10-07), stored as it is so it can also be taken out and imported as
  one space anywhere. An organization's space archives are version 2: they
  hold every page out of the trash, pages never published included, and
  each person's own draft, which a space's own export leaves out.
- `brand/logo` and `themes/<file id>`: the logo's and the themes' files.
- `audit.jsonl`: the audit log, oldest first, one entry a line.

People are named by their email address everywhere, and groups by their
name, as in a space's archive.

## Importing

**Import organization** uploads an archive, up to `STATOR_ORG_IMPORT_LIMIT`
(2 GB by default; the api writes the upload to its temporary directory, and
whatever is in front of it must let that much through). The organization it
is imported into must have no spaces yet: a fresh instance's one
organization, made by `seed bootstrap`. An organization with spaces refuses
the upload at once, and so does the worker.

The worker reads the archive through once, then writes everything in one
transaction, so an import that fails leaves nothing behind:

- **People** are found by their email address, or their account is made.
  Each is let in with the role the archive gives; somebody who is a member
  already, such as the administrator importing, keeps their own. A guest is
  let into the space they were invited to. Nobody has a password or a
  session: everybody signs in through the provider, and their account is
  found by their address at the first sign-in.
- **Groups** are found by their name, or made, with their members.
- **Spaces** come in under their own keys with their pages, versions,
  files, comments, reactions, labels, restrictions, permissions, blog,
  calendars, templates, drafts and pages never published, a personal space
  as its owner's and an archived one archived. Everything gets new ids, and
  links, includes and template buttons from one space to another are
  rewritten with them.
- **Settings**: the organization's name (its address stays this
  instance's), anonymous reading, public links, the hub page, global
  permissions, the brand's footer lines and logo, the themes and the
  default theme, the organization's templates, notification settings and
  chosen themes.
- **The sign-in provider and the Armature connection** come across only
  when the organization has none of its own; the report says when it kept
  its own. A provider comes across switched off, without its secret.
- **Webhooks** come across switched off, without a secret that signs.
- **The audit log** comes across as it was, its people and what it names
  pointed at their new ids, followed by `org.imported`.

The page shows what came across when the job is done.

## What does not come across

The archive lists these in `notCarried`, and the settings page lists them
before anybody exports:

| What | Why, and what to do |
|---|---|
| Passwords | Only a bootstrap administrator has one, from the settings of the instance it lands in. |
| Sessions | Everybody signs in again; accounts are found by their address. |
| Personal access tokens | Their secrets are not kept in a readable form. Their owners make new ones. |
| Linked Armature accounts | Each person's Armature token is sealed with the old server's key. Everybody connects again under their profile. |
| The sign-in provider's client secret | Sealed with the old server's key. Enter it, then switch the provider on. |
| Webhook signing secrets | Sealed with the old server's key. Rotate each webhook's secret, give the receiver the new one, switch it on. |
| Armature's webhook secret | Sealed with the old server's key. Save the Armature connection again and enter the new secret in Armature. |
| Requests to join | Whoever still wants in signs in again and asks anew. |
| Shares, public links and anonymous reads by link | A link opens the organization to people; give them again. |
| Notifications, stars, watches, page views and visits | Each person's own reading of the old place. |
| Owners, verifications, scheduled publishes, shortcuts, reviews and read confirmations | Arrangements rather than content, as in a space's archive. |
| A page's shared draft while people edit it together | Each person's own draft comes across, saved from it. |
| The trash | What was deleted stays deleted. |
| Jobs and their files | Exports, imports, Word imports and the example space's jobs belong to the old worker. |
| Themes mirrored from Armature | They come back when their owner follows Armature's theme again. |
| The organization's address (its slug) | The organization keeps the address of the instance it lands in. |

## Limits

Each space is held to the limits of a space's archive (5000 pages, 100000
versions, 20000 files, 8 GB unpacked); an export of an organization with a
larger space fails and names it. An archive holds at most 2000 spaces. One
export or import runs for at most four hours.
