# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-09-30: A passage is a mark in the live body, checked by the database, and found again by its words

An inline thread's passage is the mark `inlineComment` on the words in
`page.body`, not an offset kept beside the page. The editor carries a mark
along with the text it edits, so a passage survives edits around it without
the server following each keystroke, and a draft holds the marks the editor
loaded. Versions never hold the mark: a trigger strips it from every
`page_version`, as it strips hints, and a comparison takes it out of a draft
before aligning, so history reads as if there were no threads.

Somebody who may comment and not edit still changes `page.body` when they
start a thread. The service compares the body sent, without the new mark,
with the stored one, and answers 409 `anchor_conflict` when anybody changed
the page meanwhile. The database does not rest on that: `page_write_guard`
lets a body change by somebody without edit only when it adds the mark of
one new inline thread of their own on this page and nothing else, compared
by `document_without_anchors`, which joins again the text only the mark set
apart. Go's `document.DropMarks` normalizes the same way. The page's
`updated_at` stays for such a change, so a comment does not look like an
edit.

Each publish settles the marks in its transaction: a thread keeps the marks
the body carries, a thread without one is put back where its quote occurs
exactly once in one text block, and anything else is detached for good. A
detached thread is never anchored again, even when a restore brings its
words back: the words may be back in a new sense, and a thread that jumps
back is harder to follow than one that says its passage is gone. Resolved
threads are settled like open ones, so reopening one finds its passage.
Marks that name no live thread of the page are dropped, which is also what
keeps a client from inventing anchors through a draft; a copy takes none.

Code blocks take no marks, as in the editor, so their text takes no inline
comments; the contract named them among the text blocks, and a mark in code
would have needed an exception in the allowlist and in the editor for one
kind of mark. A reply to a resolved thread reopens it without a
`thread.reopened` event: the reply tells the thread's writers already, and
two notifications for one act would be one too many.

## 2026-09-30: The worker tells each person acting for them, and a digest is due by its rows

Notifications follow Armature's: an event is written to `outbox_event` in
the transaction of the change, one row per person per event is what they
were told, and a mail is a copy of that row. Three things differ.

The worker reads the table itself with `FOR UPDATE SKIP LOCKED`, inside the
transaction that marks each event done, instead of relaying it to a Valkey
stream, as the contract of M2 says. Any number of workers may run and be
restarted: one holds an event at a time, a crash before the commit hands it
to the next pass, and the unique `(event, person)` makes the second pass
write nothing. An event whose handler fails waits five seconds more for each
failure and is given up after ten, with its last error kept, so one bad
event cannot hold up the rest. Done events are deleted after a week.

Who could hear of an event is read across people as the worker's role,
through `page_watch_coverage`, which the app role may not call. Each row is
then written in a transaction of the app role acting for its recipient, so
the policy that lets a person insert only their own row about a page they
may view refuses a row that should not be. The check does not rest on the
fan-out remembering it, and the integration suite restricts a page after
somebody watched it and sees nothing reach them. Rows are never written
about a page in the trash, and the list, the count and a digest read only
rows about pages the reader may still view, out of the trash.

A digest's schedule is read from the rows: an hourly bundle is due at the
next full hour after its oldest row, a daily one at the next 08:00 UTC.
Armature keeps the last send in the worker's memory, which two workers or a
restart would each start afresh. The bundle is taken with `DELETE ...
RETURNING` in the transaction that sends it, so a second worker waits and
finds nothing, and a failed send leaves the queue for the next pass. A mail
sent at once goes after its row is committed, and a failed send is logged
and not tried again: a second pass finds the row and cannot tell whether the
mail went, and a notice twice is worse than a mail missed with the row still
in the app.

A page first published tells its space's watchers, subtree watchers above
it, and whoever watches the page right above it, since a new child is a
change of that page as its readers see it; a page watch further up covers
that page alone. Every path that publishes writes the event, copies aside,
so mentions (#24) take effect however a page is published; only publish and
restore carry `notifyWatchers`. The history's restore sends it set, as the
publish dialog offers it ticked, and its question says the watchers are
told. The preferences show the email switch off and unavailable while the
kind is off in the app, which is what the server does with it.

## 2026-09-30: A comment's words go with its delete, its place stays

A thread is a row of its own, `comment_thread`, whose id is its first
comment's, and each comment names its thread and its page, which the
foreign key holds to the thread's page. The thread row is where #23 keeps
what belongs to a thread rather than a comment: its anchor and whether it is
resolved. Deleting a comment is an update that sets `body` to null with
`deleted_at` and `deleted_by` in the same statement, and a check keeps the
two together, so a deleted comment's words cannot linger in a column the
service forgot. The row stays, so a thread keeps its shape around a
placeholder; a thread whose every comment is deleted is left out of lists
and counts. Nothing but purging the page removes rows.

Who may do what is the contract's: comment needs view and the space's
`addComments` on a published page out of the trash, and never the page's
edit list, since a comment does not change the page. Editing is the
author's alone while they may comment. Deleting is the author's while they
may view the page, or anybody's who holds the space's `delete`, which a new
space grants everyone, as it grants deleting pages. An organization that
wants only some people to remove others' comments takes `delete` from
everyone; the audit log records every such deletion either way. A policy
cannot tell an edit from a delete, so `comment_write_guard` does, as
`page_write_guard` does for pages, and the app role may update only `body`,
`edited_at`, `deleted_at` and `deleted_by`.

Deleting a comment takes back the notifications about it in the database:
an `AFTER UPDATE` trigger, `SECURITY DEFINER` because the app role may not
delete notifications and the rows are other people's. The worker may be
delivering a notification about the comment at that moment, so a
`BEFORE INSERT` trigger on `notification` takes a share lock on the comment
and drops the row when it is deleted; whichever commits second sees the
other. Both live in migration 00153, not in 00140 to 00149 with the
comments: the notification table they tie to is made by 00152, and a fresh
database applies migrations in order.

The comment editor is the page editor with a `comment` variant: the same
extensions minus the blocks the comment allowlist leaves out, so the schema
itself refuses a table, and markdown pasted into it is fitted to the schema,
keeping the words of what it cannot hold. It is loaded with the first
comment somebody starts to write, so reading a page still downloads no
editor. `api/comment-allowlist.json` is generated beside the page allowlist
and holds the editor to it in a unit test, as for pages.

## 2026-09-30: Generated blocks store their settings, never their output

A table of contents stores only the deepest heading level it lists, and a
child pages block only its scope, depth and sort. Both are drawn afresh each
time: the table from the headings of the document it sits in, the list from
`GET /pages/{id}/below` as the viewing reader may see it. Storing the list
would put titles of pages somebody may not view into a body everybody reads,
and would go stale on every move, rename or trash; storing the table would
duplicate the headings it is made from. The list route narrows itself with
`perm.ViewablePage` like every other list of pages, and stops at 500 pages,
keeping the shallowest so a cut list is still a tree.

A comparison of versions describes the blocks in words ("Child pages: all
pages below, every level, by title") rather than drawing them. Drawn, both
sides would show the page's headings and pages as they are now, not as they
were, and a change of settings would look like no change at all. Exports
have no path of their own yet: printing the page, which is how a PDF is
made, prints the reader's view, which draws both blocks.

## 2026-09-30: Templates are served by the API, and hints never get published

The built-in templates are one JSON file, `backend/internal/template/builtin/en.json`,
compiled into the api and served by `GET /templates` and
`GET /templates/{templateKey}`, in the shape Armature gives its project and
dashboard templates: a key, a name, a description and `builtIn`. Each body is
the document the editor stores, so the Go validator and the web's allowlist
test both judge the file as they judge a page, and the web client never
carries a copy of its own. Serving them rather than bundling them in the
client leaves room for an organization's own templates, which will be rows
keyed by id in the same shape and the same list. A title may hold `{date}`,
which the client fills with the local day, since only it knows the author's
time zone.

The text is English for now. Translating it will mean a file per language
beside `en.json`, with the same keys in the same order, each body written
whole by its translator rather than assembled from strings, since a
translated template may want a different structure; the API will pick the
file by the caller's language and fall back to English for a key a
translation lacks. Names and descriptions go with the bodies, so one file is
everything a translator touches.

Placeholder text is a mark, `hint`, on ordinary text. A node would have
needed a text content of its own and a way to turn into a paragraph on
input; a mark is text the editor already knows how to type into, and the
allowlist only had to learn a name. The editor takes the whole run of hint
text away on the first keystroke, paste, Backspace or Delete that lands in
it, and takes the range from the input rather than the selection, since a
browser reports the first key after a click before the selection follows.

A hint is never content, so publishing strips it rather than keeping its
words as plain text: a hint left in would read as the author's own sentence.
The database does it, with a trigger on `page_version` and on `page` once
its version is above 0, calling `document_without_hints`. Every way a page
gets published, the copy of an unpublished page and raw SQL included, is
held to it without the service having to remember. Only hinted text goes;
the label before it and the empty block it sat in stay, so the published
page keeps the template's structure. An unpublished page and a draft keep
their hints, because they are what the author is still filling in.

## 2026-09-30: A label is a word on a page, not a row of its own

Armature keeps labels in a table of the organization's words, each with a
colour, and links issues to them. Stator keeps only `page_label`, a page and
a name. A label exists while a page carries it. With a table of words,
autocomplete and a label's page would tell anybody the words used on pages
they may not view, since a word is often the most telling thing about a
page; derived from `page_label` under the page's policies, a label is seen
only with a page the reader may view, and the database holds raw SQL to the
same. What this gives up is renaming a label everywhere at once and giving
it a colour, which nobody has asked for yet.

Names are lower case, one word, spaces turned to hyphens, and letters,
digits, `-`, `_` and `.` only, where Armature keeps the case typed and takes
any character but a space. A label's name is its address, `/labels/{name}`,
so `Release` and `release` must be one label and no name may need escaping
or read as a path. The database checks what it can without depending on its
locale: lower case, no leading punctuation, and none of the characters that
break an address; the service checks the rest.

Label changes are not audited, because the other page edits are not.

## 2026-09-30: The compose stack's commits do not wait for the disk

The primary in `deploy/docker-compose.yml` runs with `synchronous_commit=off`.
On a host running several gates, a single fsync of the WAL took up to about
9 s, and every commit waited for it: removing a member once took 8.7 s, longer
than any timeout the browser suite allows. With the setting off, a commit
returns once its WAL is written, and the WAL writer flushes it shortly after.
A crash of the machine can lose less than a second of writes, but never
consistency: what survives is a prefix of what was committed. The replica
does not take the setting, since nothing commits there. It still replays
only what the primary has flushed, and read-your-writes already sends a read
to the primary while the replica lags. The integration suite's `settle`
flushes the WAL before it waits, since the write position it used to wait for
can stop short of a commit that is still in the buffers.

This is for the development and test stack alone. Production, and every
layout of the Helm chart, keep the default, and `tests/test-helm.sh` fails if
a rendered chart names `synchronous_commit`. Nothing else about durability
changes.

## 2026-09-30: A refused write shows its caller what refused it

A publish conflict tells its caller that somebody else published since their
draft began. The editor then reads the page to learn that version's number,
and offers to compare it with the draft. The refusal was decided on the
primary, but the reads after it were held only to the caller's own last write,
which came before the other publish, so a replica that had not replayed that
publish could answer them: the editor offered "Compare your draft with version
1" while version 2 had refused it, and the comparison could miss version 2.

`Cluster.Write` therefore returns, with a refused write, the primary's
position at the refusal, and the handler notes it under the caller's key as it
notes any write. What the caller reads next is then at least as new as what
refused them. This keeps read-your-writes keyed by session: nobody's reads wait
for another person's write, unless the API has just told them of it.

The other way was to put the current version in the 409's body. It was not
taken because the error envelope is one shape for every refusal, and a number
in it would serve the dialog but not the comparison the dialog leads to, which
reads that version next. Every refusal that speaks of newer state, a stale
`PATCH` or restore among them, is covered the same way without a field of its
own. The price is one more query on the primary for each refused write.

## 2026-09-30: Search reads a page's words in the database, and trims in its SQL

A page's title, weighted A, and the plain text of its body, weighted B, are a
stored generated `tsvector` column on `page` with a GIN index. The plain text
comes from `page_plain_text`, a SQL function that reads the document as
`document.PlainText` does, rather than from the service: a generated column
cannot call Go, and a text column the service kept would be stale after any
writer that forgot it, raw SQL and the migration's own backfill included.
The integration suite holds the two readings to the same answer. The body's
text is cut at 200000 characters, since a `tsvector` holds at most 1 MB of
lexemes and a page may be 2 MB.

The configuration `stator_search` is `simple` with `unaccent`: case and
accents are ignored, words are not stemmed, as in Armature, because one
organization writes in more than one language. Queries go through
`websearch_to_tsquery`. A hit whose title matches every word comes before one
that matches only in the body, then `ts_rank`, then the latest change, which
is the latest published version, so moving a page to another place in the
tree does not count as changing it. Quick search asks for each typed word as
the prefix of a title lexeme (`'word':*A`), built from letters and digits
only, so nothing typed reaches the tsquery syntax.

Titles and snippets are `ts_headline` output with two private use
characters as delimiters, stripped from the text first, then split into
`{text, match}` runs; no markup ever leaves the server. Visibility is a
condition inside the query that finds the hits, so `total` never counts a page
the caller may not read. It is `perm.ViewablePage`, the rule every list of
pages uses, and the restrictive policies of #19 hold raw SQL to the same. A
person's visits are their own: `page_visit_viewer` lets the app role read
and write only the actor's visits, and only of pages they may still view.

Files are found by name only. Their bytes are in the bucket, and reading the
text of plain text files would mean fetching every upload into the database
and holding a second copy there; a name is what people search a file by. The
name is indexed as written and with each run of punctuation as a space, since
the parser would read `plan_v2.pdf` as one path. A file follows its page: on a
page the caller may not view, an unpublished page or one in the trash, it is
not found. Pages and files are ranked together, a file counting as a title
match.

## 2026-09-30: Permissions are rows, rules are SQL functions, and the database knows who asks

Grants are rows, as in Armature: `global_grant` for `use` and
`createSpace`, `space_grant` per space and permission, `page_restriction`
per page and list, each naming a person, a group or (for grants) everyone.
`administer` of the organization is not a row: it is the owner and admin
roles, so it can neither drift from them nor be taken from the last
administrator. Organization administrators hold every permission in every
space, so no space is ever orphaned, and nobody without `use` holds
anything. A new organization grants everyone `use`, and a new space grants
everyone view, add pages, add comments and delete and its creator
administer, by triggers, so a space made by any path starts the same; the
migration gave every existing space the same, which was the behaviour
before.

Each transaction names the person it acts for in `app.user_id`, beside
`app.org_id`, set from the request by `db.WithUser`, and the rules are SQL
functions of that person: `perm_space_holds`, `perm_page_viewable`,
`perm_page_editable` and the ones they call. The policies call them, and the
service does too: every list of pages narrows its query with
`perm.ViewablePage`, which is `perm_page_viewable`, so the tree, the outline,
`hasChildren`, the trash, copies and search cannot disagree about what a
person sees. The service decides single acts in Go (`perm.Decide`,
`perm.PageRules`) from facts the same functions read, which keeps the rules
testable alone and lets it answer 403 or 404 in a sentence; an integration
test holds the Go rules and the SQL ones to each other page by page.

A view restriction hides the page and every page below it, an edit
restriction stops editing and deleting them, and a person has to pass every
list on the page and above it. That makes inheritance a walk up the tree at
read time instead of copies of the lists kept below, so a move takes on its
new parents' lists and keeps its own without rewriting anything. A save that
would leave its saver unable to view or edit the page is refused unless they
administer the space; the home page takes no view list, which is what the
space's view is for.

What the database enforces, for `stator_app`, with restrictive policies and
one trigger:

- reading spaces, pages, versions, drafts (one's own only), files,
  restrictions and grants follows the same rules as the service, `use`
  included;
- putting a file on a page or taking it off needs edit of the page;
- writing a page's content or place needs edit, its trash marks need delete,
  purging needs administer of the space, and a new page needs edit of its
  parent, which `page_write_guard` tells apart since a policy cannot see
  which columns an update changes;
- versions and drafts need edit, restrictions need edit of their page and no
  view list on a home page, space grants need administer of the space, global
  grants an organization administrator, and a new space `createSpace`.

Moves of pages the actor cannot see, which trashing a subtree, restoring an
item and reordering siblings make, go through a few `SECURITY DEFINER`
functions (`page_trash`, `page_untrash`, `page_place`, `page_purge`,
`space_empty_trash`, `page_sibling_ranks`), each of which checks the rule for
the page the actor named, taking the actor from the transaction.

What only the service enforces: that a restriction save does not lock its
saver out, and every answer's shape, such as 404 rather than 403 for what
may not be seen. And what nothing below the api can enforce: `app.user_id`
is set by whoever holds a `stator_app` connection, as `app.org_id` is, so the
database holds a connection to the person it names but cannot tell a forged
name. That credential is the api's alone, and the policies turn a query
that forgets whom it is for into one that sees nothing.

## 2026-09-29: A deleted file leaves a tombstone, written by the database

Attachments work as in Armature. The bytes go to the bucket inside the
transaction that writes the row, so a refused upload leaves no row. A
deleted row leaves a tombstone, and the bytes are removed after the commit,
by the request that deleted it or by the worker's reaper. Armature writes
tombstones in the service. Here a trigger on `attachment` writes them, so
every path that removes a row leaves one: a delete, a purged page, emptied
trash, a deleted space or organization, and raw SQL. Purges and space
deletes then sweep the organization's tombstones before answering. The
tombstone table has no foreign key to `org`, so an organization's
tombstones outlive it until the reaper has emptied its prefix, which is
what Armature arrived at with its migration 00900.

The object key is a generated column, `org/<org>/page/<page>/<id>`, and a
tombstone must name a key under its own organization's prefix. The app role
has no UPDATE on either table. Without these, a tenant could point a row or
a tombstone at another tenant's object, and the reaper, which works across
tenants, would delete it. Files are copied with their page by reading and
writing each object inside the copy's transaction, as an upload does. A
copy that fails after writing some objects leaves them unreachable in the
bucket, which costs space but breaks nothing.

## 2026-09-29: History is append only, and a comparison aligns blocks, then words

`page_version` rows are written once. The app role may only read and insert
them; they leave with their page, by the cascade. A trigger refuses any
number but the page's current version plus one, so numbers have no gaps
whoever writes them, and it locks the page row, so two publishes queue.
Publishing inserts the version, then copies it onto `page`, in one
transaction. A draft is keyed on page and person, and references the
membership, so leaving the organization takes a person's drafts with it.
The database walls drafts off by organization, and since #19 by person too.

An unpublished page is `version = 0`, and whether somebody may see it is its
`created_by`, checked for the page and every page above it wherever a page is
read. A copy of a subtree leaves out what the copier cannot see.

A comparison first matches identical top-level blocks by their longest
common subsequence. Between two matches, blocks of the same type are paired
the same way and compared inside: text blocks word by word, where a word
whose marks changed counts as replaced; lists, quotes and panels child by
child, a child added or removed inside them marked whole; tables cell by
cell, but only while every row has the same cells, else the table is
deleted and inserted. A heading's anchor and a cell's width and colours do
not stop a comparison. Past about four million cells an alignment gives up
on the middle and shows it replaced, so a huge rewrite cannot exhaust the
server. The draft side of a comparison is number 0 with `draft: true`,
written by the caller.

## 2026-09-29: A deleted page stays in place, marked, until it is purged

Deleting a page marks it and every page below it still in the tree with
`trashed_at` and `trash_id`, the page deleted, which makes them one item of
the space's trash. Nothing moves: parent and rank stay, so a restore clears
the marks and the item is back where it was. If the page it was under went
to the trash itself, or was purged, the restore hangs it last under the home
page instead. Purging an item first moves items deleted earlier from below
it under the home page, so they stay restorable, then deletes the item's
rows. The alternative, a trash table the pages move into, would copy every
column a page gains, drafts and versions (#13, #14) included, and lose the
place a restore needs. Everything that reads the tree, and search and macros
when they come, leaves out `trashed_at IS NOT NULL`; `page.load` already
answers a trashed page as not found.

## 2026-09-29: Siblings are ordered by lexicographic ranks, loops refused by the database

A page's place among its siblings is a rank from `internal/rank`, ported
from Armature, whose byte order is its position: dropping a page between two
others writes that page alone. The column is `COLLATE "C"` so Postgres sorts
it as the generator compares. Ties, which only a lost race can leave, are
ordered by id and renumbered when a page is dropped into one.

Subtrees are read with recursive queries rather than a stored path, so a
move writes one row, and a move into another space rewrites the page's space
and parent in one statement that the foreign keys cascade down the subtree.
A trigger refuses any parent that is the page or below it, so a loop cannot
be written by any path, raw SQL included. Two moves that are fine alone can
close a loop together, so each change of a parent first takes an advisory
lock per space, and the service takes the same locks before any row, which
makes two crossing moves queue instead of deadlocking.

## 2026-09-29: Who may do what is decided in one place until permissions arrive

Space and page permissions (#19) come later. Until then every member of an
organization reads and edits every page of every space, and only its owners
and administrators create, rename and delete spaces. That rule lives in
`internal/perm` and nowhere else: every service calls `perm.Check` with the
actor, the action and the space, inside the transaction that acts, and the
interface reads the same answers from each space's `can`. #19 replaces the
body of `perm.Check` with lookups of its own tables, in that transaction,
without touching a caller. The database walls tenants off from each other
today; it does not yet know roles, so a member's raw SQL within their own
organization is refused only once #19 adds policies for it.

## 2026-09-29: A page row is the page, and versions will hang off it

`page` holds what every reader and the tree need: the space, the parent, the
rank among siblings, the current title and body, and `version`, which counts
saves so a save made from an older copy is refused. Until drafts and
publishing (#13) the editor saves straight over the body.

Versions and drafts will be tables of their own keyed on `(org_id, page_id)`,
referencing `page (org_id, id)`, a unique key that exists for them, with
`ON DELETE CASCADE`. Publishing writes a `page_version` row and copies its
title and body onto `page` in the same transaction, so `page.version` becomes
the published version's number and nothing that reads a page joins the
history. History (#14) reads `page_version`; restoring one publishes it
again. A page nobody has published yet needs one column added to `page`, not
a new shape.

The home page is the root of its space's tree, and constraints keep it so:
one page per space has no parent, a parent is always in the same space and
organization, and a space's home is one of its own pages. A foreign key is
checked past row level security, which is why every one of them names the
organization too.

## 2026-09-29: Each browser spec file gets a throwaway organization

Specs that change what an organization shows, such as its default theme or
who uses which theme, cannot share `demo` with specs that judge the built-in
look, and running them one after another only hides the problem. The api
therefore has two test endpoints, `POST /api/v1/test/orgs` and
`DELETE /api/v1/test/orgs/{slug}`, and `e2e/fixtures/org.ts` makes one
organization per spec file and worker through them, signs alice and bob in to
it, and deletes it when the worker stops. The new organization gets the seed's
people and provider, from the same `STATOR_BOOTSTRAP_*` settings and the same
code, so it signs in exactly like `demo`.

The endpoints are compiled in, because the image the suite runs is the image
that ships, but routed only when `STATOR_TEST_ENDPOINTS` is on; otherwise they
answer 404 like any path that does not exist. Every call must carry
`STATOR_TEST_ENDPOINTS_TOKEN` in `X-Stator-Test-Token`, so an endpoint switched
on by accident is still closed. The api refuses to start with them on in
production, and `tests/test-helm.sh` checks that no layout of the chart sets
either variable. Deleting refuses any organization the endpoint did not make,
which it marks in `org.settings`.

They are left out of `api/openapi.json` rather than marked in it. The document
is the contract clients are generated from, and the web client would
otherwise carry types for calls it must never make; a separate table in
`internal/httpapi` describes them, and a unit test holds the router to both
tables and the document to the public one alone.

Deleting an organization has to find its files, so every object key now starts
with `org/<organization id>/`, and the store can list a prefix.
## 2026-09-29: Provider groups decide roles at each sign-in

An administrator maps groups of the identity provider, by the value of the
groups claim, to member or admin, per provider. Each sign-in through the
provider settles the person's role from the groups the token names: the
highest mapped role wins, and a membership records whether its role came from
the provider or from somebody here. A role the provider gave falls back to
member when its group goes, never further, since taking somebody out is an
administrator's act. A role somebody here chose is left alone while none of
the person's groups is mapped, so a mapping can be introduced group by group
without undoing what administrators already decided. The owner is never moved,
and the database refuses to let the provider manage an owner's role.

A mapped group is the administrators' approval given in advance, so somebody
in one joins on their first sign-in, with no request to answer. Every role the
mapping changes, and every change to the mapping, goes to `audit_log`.

Changes apply at the next sign-in rather than when the mapping is saved: only
a token says which groups somebody is in now, and group rows exist only for
the groups an organization keeps. Until then an open session keeps the role
it had.

## 2026-09-29: A personal access token is its owner in one organization

A script calls the API with a token sent as a bearer, as in Armature: the
prefix `stator_pat_` and 32 random bytes in base64url, of which only the
SHA-256 is stored, so a copy of the database opens nothing and a secret
scanner recognises a leaked one. A token belongs to a person in one
organization and reaches nothing else; leaving the organization removes it
by a foreign key onto the membership. Its one scope, `read`, is enforced by
middleware in front of every route, so a route added later cannot forget
it. Only a session makes a token, so a leaked token cannot mint a longer
lived one and outlive its own revocation. Tokens key read-your-writes as
`t:<token>`, apart from their owner's browser, so a script's writes do not
send the person's reads to the primary. Narrowing a token to some spaces is
left for later (#111).

The integration suite checks every answer the API gives it against
`api/openapi.json`, and fails when an operation was never answered
successfully or never refused, as Armature's does. The router is wrapped
rather than the test client, so the sign-in tests' browsers are checked too.

## 2026-09-29: Reads go to CloudNativePG's -ro service

On Kubernetes the chart points reads at the CNPG cluster's `-ro` service
instead of at each replica by name. The service follows the operator through
failovers and scaling, which a list of pod names would not; the price is that
one pool's connections reach different replicas. The api therefore checks
replay position and lag on the connection each read gets, and the health loop
samples several connections per pool, so the service is judged by more than
the one replica a single connection happens to reach.

## 2026-09-29: Read-your-writes is keyed by session, else by a client cookie

A write's position is kept in Valkey for `STATOR_READ_YOUR_WRITES_TTL` under a
key naming who wrote, and that key's reads stay off any replica that has not
replayed it. The natural key is the session, as in Armature, but sign-in
(#5) has not landed and there are no sessions yet. Until then, and for any
request without a session later, the key is a `stator_client` cookie: a
random id the API hands to a request that may write and has none. It is
http-only, carries no rights, and only ever sends its holder's own reads to
the primary, so forging one gains nothing. Keying by user instead would pin
every device of a person to one another's writes and could not tell two
anonymous callers apart; keying by IP address would pin whole offices to
one person's writes. Once sessions exist, `auth.Principal.SessionID` wins
over the cookie with no other change.

The position is recorded when the handler notes it, before the response is
written, and even for a refused request, because the caller's next request
can arrive before the handler returns and a refused import has already
written and undone a theme.

## 2026-09-29: The test stack keeps the streaming replica

The compose stack runs Postgres as a primary and a real streaming replica, as
Armature's does, and the gate runs against it. Read routing, the replica
health check and read-your-writes only mean something against real
replication, and a replica costs the gate a base backup of an empty cluster:
a few seconds, where Keycloak's start already takes longer. The integration
suite now uses this stack instead of a Postgres and a SeaweedFS of its own, so
the gate runs exactly one of each.

## 2026-09-29: An administrator lets each person in, and a session stays home

Signing in through the organization's provider is not the same as being let
in, as in Armature. Somebody the provider vouches for who is not a member gets
no session: the sign-in is refused, their account and a request to join are
noted, and the sign-in page tells them the request waits for an
administrator. An administrator lets them in as a member, or turns them away,
under Single sign-on; either answer is written to `audit_log`, which the audit
log will read. Letting anybody with an account at the provider in would leave
an organization with no membership at all, only a sign-in page. A development
stack names its people ahead of time with `STATOR_BOOTSTRAP_MEMBERS`, so
nobody has to click. For members, provider groups follow the groups claim
exactly, so revoking a group there revokes it here on the next sign-in; which
groups grant which role is a later decision.

A person is matched by issuer and subject, which survive an email change. A
first sign-in whose verified address already has an account, such as the
bootstrap administrator's, is tied to it. Because any organization may point
at a provider of its own choosing, a session opened through a provider only
reaches the organizations that trust the same issuer, and ones the person
owns; a password session reaches every membership. The database enforces
this with `session_reaches` and a trigger, not only the service.

## 2026-09-29: Only a bootstrap administrator has a password

Sign-in is through the organization's provider. A local password, hashed with
argon2id, exists so a fresh deployment can be entered and its provider set up;
it comes from `STATOR_BOOTSTRAP_ADMIN_EMAIL` and `_PASSWORD`, applied by
`cmd/seed`. Session tokens are 32 random bytes of which only the SHA-256 is
stored, and an identity provider's client secret is sealed with
`STATOR_SECRET_KEY`, bound to its organization.

## 2026-09-29: Armature is reached as the viewing user

Every call to Armature uses the viewer's own personal access token. A shared
bot token would be simpler, but it would show issues on a page to people who
cannot see them in Armature. Users without a connected token see issue keys
only.

## 2026-09-29: A stub stands in for Armature in the test suite

Running a full Armature stack in every gate would double its time. The
`armature-stub` implements only the operations Stator calls, and a contract
test checks it against Armature's published `api/openapi.json`, so the stub
cannot drift from the real API unnoticed.

## 2026-09-29: Playwright runs in the push gate

End-to-end tests run in `./run-tests.sh` before every push and in CI, not only
nightly. Ports and compose project names derive from the checkout path, so
parallel worktrees can run the gate at the same time.

## 2026-09-29: Multi-tenant by organisation, enforced by the database

Like Armature, every table is scoped to an organisation through row-level
security. A missing `WHERE` clause cannot leak data between organisations,
and tests prove it with raw SQL, not only through the service.

## 2026-09-29: The stack mirrors Armature

Go with chi, pgx and goose; PostgreSQL; React with TanStack and Tailwind v4;
OIDC for sign-in. Reusing Armature's patterns and design tokens makes the two
products look and behave as one, and lets code move between them.

## 2026-09-29: AGPL-3.0

Anyone may run and modify Stator, but whoever offers a modified version as a
service must publish their changes. Code adapted from Armature stays under its
Apache-2.0 notice (see `NOTICE`), which is compatible with the AGPL.

## 2026-09-29: Every change hangs off an issue

Work happens on `<type>/<issue>-<slug>` branches and lands through pull
requests that the maintainer merges. Hooks enforce it and run the full suite
before every push.
