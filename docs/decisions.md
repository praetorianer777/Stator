# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-10-06: A blog post is a page outside the tree, dated by its first publish

Blog posts (#72) are rows of `page` of the kind `post`, as a folder is a
row of another kind, not a content type of their own. A post then has
drafts, publishing and scheduled publishing, versions and comparisons,
comments, reactions, labels, restrictions, sharing, search, the trash, the
archive and reading without signing in exactly as a page has them, through
the same rows, rules and screens, and a later feature of pages reaches
posts without anybody remembering them. Its address is a page's address,
so every link, mention, notification and search hit leads to it as it is.

A post has no parent: it lives in its space's blog, beside the tree rather
than in it, and no page hangs under it. The database holds both
(`page_post_outside_tree`, and a trigger refusing a post as anybody's
parent); the one root of a space is its home page whatever kind has no
parent, and a post goes to the trash, the archive and under a view
restriction as any page below the home page does. Moving, copying,
importing Markdown under one and making one live answer `409 post`: a post
goes out once, on its date, which a live page amending itself every few
seconds would make meaningless.

Its date, `page.posted_at`, is stamped by the database when its first
version is published and never moves after; the app role cannot set it. A
date set by hand would let a post be filed under a month in which nobody
could have read it, and watchers would be told of something "posted" a
year ago. A post wanted for later is scheduled, and its date is then the
time it goes out. So a post is written unpublished, at version 0, which the
insert policy demands of the app role, and its first version is a real
publish with a row in the history. Whoever may add pages to a space may
post in it, while the space is not archived (`perm_post_insertable`); the
author a list names is whoever published the first version.

The blog is `/s/{key}/blog`, and its date navigation is by year and month
with the count of posts the reader may read in each, newest first, from
`GET /spaces/{key}/blog`, which also lists the caller's own posts still to
go out and whether they watch the blog and may post. Months are counted in
UTC, as a date node and a task's day are, so every reader files a post
under the same month. `GET /posts` lists the posts a reader may read,
newest first, in one blog or across the spaces not archived, a year or a
month at a time, a window at a time by keyset; posts in the trash or the
archive are left out, as the lists of pages leave them out. Each carries
its opening words, cut after a whole word, read from the published body.

Watching gains a kind, `blog`, on a space beside the watch on the whole
space. It hears of a post when it is first published and of nothing after,
as a watch on a page hears of a new page published under it; whoever wants
a post's later versions watches the post, which its author does by their
own publish. A watch on the space still hears of everything in it, posts
included. Either hears a new post as the notification kind `posted`, with
its own switch in the preferences, since a team announcement and a new
page somewhere in the tree are told apart. Whom it reaches is decided as
for every publish, per person as that person, so a post restricted to a
few is told to those few. A post written and published at once, as an
assistant may, tells the watchers, while a page made published at once
does not: a post sent out is an announcement. Webhooks keep their topics:
`page.published` now says the page's `kind`, and version 1 of a post is
its going out.

The latest blog posts block stores a space's key, or null for every space,
and how many posts to list, never the posts. "This space" in its dialog
stores the page's own key, as a recently updated list does, so a copy or a
move of the page keeps listing the blog it was made for. Each reader's
view asks `GET /posts` as that reader, so a restricted post, a space they
may not read and an unpublished post stay out wherever the block is shown,
in an include or an excerpt too; a comparison and the reading view of
somebody not signed in say in words what it lists, as for every block
whose content is each reader's own.

Reading the blog and writing a post are MCP tools (`get_blog`,
`list_posts`, `create_post`), as reading and writing pages are; watching a
blog is not, as no watch is. Two things are left for later: the reading
view for people not signed in has no blog of its own, so a public post is
found by its address and the public search, and the home page's watched
updates do not take blog watches into account.

## 2026-10-06: A scheduled publish is the author's own publish, made by the worker at its time

Scheduled publishing (#71) sets a time at which an editor's draft of a page
is published (`PUT /pages/{id}/schedule`, from the publish dialog's "At a
set time"). The worker then does what the author's own publish would do at
that moment, acting for them through the app role: the draft as it stands
then goes out, not a copy taken when it was scheduled, so the author keeps
editing until the time; the version carries their name and the comment and
notice they chose; the watchers, the newly mentioned, webhooks and
Armature's links hear of it as of any publish. A page nobody edited yet
gets a draft of itself when its first publish is scheduled.

A schedule is a row of `page_schedule` hanging off the draft it publishes,
by a foreign key that cascades. Whatever takes the draft takes the
schedule: publishing it by hand, discarding it, the page going live, the
page purged, the author leaving the organization. One per page, by its key:
two drafts scheduled over each other would leave the second to fail on the
first, and editors should see one plan. While one waits, scheduling is
refused with `schedule_taken` naming its author; any editor may call it off
(`DELETE /pages/{id}/schedule`), as any editor may publish over it, and the
draft stays. Moving it is its author's, by setting it again. Calling off
somebody else's schedule is not audited, as no page edit is.

The page answers its schedule to its author and its editors only; a reader
learns of a publish when it happens. The page shows a note: when the
caller's draft goes out, with Change and Cancel, or whose publish is
scheduled for when. Times are instants, stored as `timestamptz`; the
browser sends the time the person chose in their zone with its offset, and
shows it back in their zone, naming it. A time must be ahead and within
`page.MaxScheduleAhead`, a year.

When the time comes and the page refuses the publish, nothing is published
and the schedule stays, marked with why (`gone`, `forbidden`, `archived`,
`conflict`): its author lost edit, the page was archived, trashed or is no
longer theirs to read, or somebody published after the draft began. The
author is told once, with the notification kind `failed`, and the note says
why to them and to the editors. Setting it again clears the mark; an
editor may schedule their own over a failed one. Since publishing over
another publish is the conflict a person resolves by comparing, the worker
does not publish over it.

The database holds the rules a request could skip. As `stator_app`: a
schedule is inserted only for one's own draft (the key reaches the draft),
only by an editor of the page, only for a time ahead, never marked failed;
it is moved only by its author while an editor, read by its author and the
page's editors, called off by either. Only the worker, as the admin role,
records a failure. The integration suite tries each of these through SQL.

The worker's part, `page.ScheduleWatch`, looks every
`STATOR_SCHEDULE_CHECK_INTERVAL` (30 seconds; a second in the compose
stack) for schedules due across organizations, as the verification watch
does, and publishes each in a transaction acting for its author that first
takes the schedule's row with `FOR UPDATE SKIP LOCKED`. The publish deletes
the draft and with it the schedule in that same transaction, so any number
of workers, on any number of replicas, publish it exactly once: a second
worker skips the locked row, or finds it gone once the first commits. The
row's update policy names its author alone, so the lock is taken even for
an author who lost edit, whose publish is then refused and recorded. A
refusal rolls the publish back and records the failure in a transaction of
its own, once however many workers were refused. Due means
`publish_at <= now()`, so a time that passed while no worker ran goes out
when one returns, once, late rather than never.

## 2026-10-06: A live page is its open version, amended by every save for ten minutes

Live pages (#70) are a mode of the page, `page.mode`, draft or live, not a
kind and not a space setting: a team keeps working notes live beside
pages it publishes with care. Whoever may edit the page chooses the mode,
in a dialog of the page's menu, as with how it looks; editors can already
publish anything and throw the shared draft away, so the mode asks no
more. A trigger holds the app's role to edit, which also freezes the mode
of an archived page, and a folder cannot be live.

A live save (`PUT /pages/{id}/live`) is the whole title and body as the
editor holds it, sent about a second after the last keystroke as a draft's
autosave is. It goes into the page's open version: its latest, saved live,
and begun less than `live_version_span()` ago, ten minutes, which
`page.LiveVersionSpan` and a test hold to the same. When there is none it
publishes the next version, marked live, which then stays open. So the
history reads in steps of work and every version is still a full,
comparable, restorable body, while one per save would bury it. The span is
counted from the version's start rather than from the last save, so a long
session still leaves a version every ten minutes. Everybody who saves into
a version is a row of `page_version_editor`, shown in the history beside
its author and counted by the contributors block, since two people typing
together would otherwise either take turns opening versions or lose one of
their names. A save that changes nothing writes nothing.

History stays append only except for that one version. The app role may
update only the title and body of `page_version` (a column grant), only
where a policy finds the row live, the page's latest and live, the span
not over and the actor an editor of the page; inserting a live version
into a page of drafts is refused, and nobody names anybody else as an
editor, or names themselves on a closed version. Integration tests try
each of these as `stator_app` straight through SQL.

A live page has no drafts: `page_draft_not_live` refuses one, and drafts
and publishing answer `page_live`. Going live throws every draft of the
page away, by a trigger that runs as the table's owner since row level
security hides other people's drafts from the person switching. So the
switch first asks `page_pending_drafts`, which tells only an editor of the
page whose drafts differ from it, and refuses with `drafts_pending`
naming them unless `discardDrafts` confirms; drafts that say what the page
says go without asking. A draft is the only place unpublished work lives:
everybody in a shared draft saves it into their own draft as they type, so
the shared draft needs no question of its own. Going back to drafts keeps
the history and starts nobody's draft. Either switch throws the shared
draft away, so editors open in the mode the page is in, and is recorded in
the audit log with the names of the drafts that went.

Editing together carries on as before: the shared draft is seeded from the
page, never from a draft, and each editor saves the shared document live
after their own changes, naming the room. Since a save goes over the page,
something else that wrote the page meanwhile, a restore, a Markdown import
or a script's update, would be undone by the room's next save; so a save
from a room whose base the page has moved past throws the room away,
answers `room_gone`, and its editors load the page afresh. That write wins
over the room's last unsaved second. A save from an editor working alone
throws a room away too, so the room's editors load what it saved.

What a save tells: a new live version is announced as a quiet publish
(`page.published` without the watchers), so webhooks and the newly
mentioned hear of each version; a save into the open version emits
`page.amended`, which tells only whom that save newly mentioned or
assigned, read by the version's body before the save and by the tasks
stamped in its transaction, and which webhooks do not carry. Armature's
links follow a save only when the issues it names changed. Checklist
items get an id in a live editor as they are made, since the server
matches an item without one to its task by its words and a task retyped
over several saves would otherwise become a new task each time.

Readers see the page as it stands, the latest save included, and a reader
with a live page open asks for it again every three seconds
(`LIVE_PAGE_REFRESH_MS`), so new words arrive without a reload and a
replica short of a save is answered by the next ask.

## 2026-10-06: A copy of space permissions applies the preview its caller saw

Copying permissions (#82) takes the source space's table onto the target
in one of two modes. Replace makes the target's grants the source's.
Merge adds, per subject, every permission the source grants and the target
does not: a subject the target lacks is added, one it names is widened,
and nothing is narrowed or removed. Merge widens rather than only adding
subjects, since a subject named in both with less here is the commonest
reason to merge, and a merge that might still leave somebody short of the
source would need a third explanation. Reading without signing in, the
grant to `anonymous`, is copied like any other subject, so a replace makes
the target as open as the source; it is shown as its own row.

The plan is `perm.PlanCopy`, a plain function of both tables, and the
preview (`GET /spaces/{key}/permissions/copy`) and the copy
(`POST`) both call it, so the diff the person reads is the one applied. The
preview lists per subject what is added, widened, narrowed, changed (some
gained, some lost) or removed, with before and after, and what is left
behind and why. A guest of the source is never copied, since a guest
belongs to the one space they were invited to and the database refuses
them anywhere else; a replace keeps the target's own guests as they are,
since the source cannot name them and they come and go by invitation.
Subjects that no longer exist cannot appear: a grant goes with its person
or group by cascade. The preview carries a fingerprint, a SHA-256 of the
mode, both spaces' ids and both tables (subjects, guest marks and
permissions, not names), and the copy recomputes it under a lock of the
target's row and a share lock of the source's, which every rewrite of a
table takes too, and answers `409 copy_changed` with a sentence when it
differs, so a change made between looking and applying is shown before it
is applied. The copy is one transaction and one audit entry,
`space.permissions_copied`, naming both spaces, the mode and the counts; a
copy that changes nothing writes nothing.

Who may: whoever administers the target and may read the source's
permissions, which only its administrators may, as the space's own reads
already decide; the database's policies hold both, so a target
administrator who does not administer the source reads none of its grants
through SQL either. Personal spaces are neither source nor target: copying
from one would make its owner an administrator of a team space by side
effect, and copying into one would rewrite its owner's private sharing,
which they keep in its own table; reading without signing in, which a
personal space never allows, is then never offered to one. Page
restrictions are not copied: they belong to pages, which a space's
permissions do not name.

A copy that would leave the target without an administrator of its own
where it had one is refused (`409 no_administrator`), and the preview says
so beforehand. The database now holds the same for every rewrite of a
table: a deferred constraint trigger refuses a transaction that takes
administer from the last subject holding it in a space, unless the
subject or the space went with it, or the actor administers the
organization. Organization administrators keep the power of 2026-09-30 to
close a space down to themselves, since they hold every space anyway; a
space administrator who is not one can hand the space on but no longer
give it away to nobody, which used to leave it to whoever administers the
organization without anybody choosing that. The copy refuses even an
organization administrator, since a copy that ends with nobody
administering is a mistake rather than a choice.

Neither operation is an MCP tool: permission reads and writes are
administration (2026-10-01), and a preview is only worth something to the
person who then applies it.

## 2026-10-06: Text keeps a measure while wide blocks break out of it

A page is one sheet as wide as the content area, up to 96rem
(`PAGE_MAX_WIDTH_REM`). Its text, the title, the header and the sections
below the page are held to 44rem (`PAGE_MEASURE_REM`), centred: at the
14px root that is about 85 characters of Inter, past which the eye loses
the next line. Blocks whose content is wide take the whole sheet: tables
and their charts, diagrams, math blocks, code, columns, link cards and
embeds, Armature charts, roadmaps and issue lists, calendars and property
reports. A picture takes its own width, no less than the measure so that
a small one starts where the text does. 96rem stops there because a row
longer than that is hard to follow from end to end.

It is done once, in CSS, on the document's top level: every block of
`.doc-content` keeps the measure unless its class, or its editor node's
`node-<name>` class, is on the list of wide ones. So the reader, the
editor, a version, a comparison and the public view lay out alike, and a
block nobody listed stays at the measure rather than spilling out. The
widths reach the CSS as custom properties from `pageSheet`, which is the
only place they are read; outside a page nothing narrows. Blocks inside a
panel, an expand, an excerpt or an include stay within that frame.

Full width sets the measure to the whole sheet and lifts the maximum, so
text widens too. A phone is narrower than the measure, so it is unchanged.

## 2026-10-06: A public link is the anonymous reader holding one page more

A public link (#80) lets anybody read one published page without an
account. It is built on #79's anonymous reader rather than beside it: the
reads run as the same principal, and the transaction also sets
`app.page_link` to the SHA-256 of the token the address carries.
`perm_link_page()` turns that digest into the page of a live link (neither
revoked nor run out, while the organization allows links), and
`perm_page_viewable` lets an anonymous reader view that page as though its
space were open: published all the way up, out of the trash, under no view
list. Every other rule of #79 still holds, so the link reaches that page
and the files attached to it and nothing else, neither the pages below it,
its space, versions, comments nor any person. Straight through SQL as
`stator_app` a token reaches exactly its page and its files, and a token
revoked, run out, unknown or malformed, a page restricted later, or links
turned off reach nothing. The digest is a better setting than the link's
id: an editor who saw the id in the list cannot read through it, and a
copy of the database opens nothing.

The token follows Armature's share links: 32 random bytes from
`auth.GenerateToken`, kept only as a digest, shown once in the answer that
makes it, never in the list; a revoked link keeps its row with who revoked
it, so who opened what to whom stays on record; the access log and the
traces write the path with the token blanked. Unlike Armature's, the
address names the organization, `/public/{org}/link/{token}`, so the
reading view and its routes stay under #79's prefix and every rule there
(no session looked at, public `GET`s only, no tool) holds for them. A token
of another organization's link is gone under this one's name.

Who makes and revokes links is whoever may change the page, or could but
for its being archived: a link gives access, which a share does not, so it
takes the right that decides what the page says, not the right to read it.
Any of the page's editors may revoke any of its links, so a link survives
its maker leaving and is still somebody's to end. A guest, who is from
outside, makes and sees none. A page has at most five live links
(`page_link_max()`, held by a trigger with a lock as the share brake is),
each with an optional label to tell them apart and an optional expiry; the
dialog offers a day to three months, thirty days first, or none.

A restricted page gets no link, and a link stops working while a view list
applies on the page or above it: a view list names who may read a page,
and a link to anybody would quietly undo it. Lifting the list brings the
link back. A draft, a folder, which only lists pages the link does not
open, a page in the trash and a page of a personal space, which is never
public as in #79, get none either. The answer is the page's words through
`document.ForAnonymous`, its title, appearance and date, without its
space, tree or people.

The organization's own switch, `org.public_links`, is apart from #79's
reading switch, since opening every space and letting editors open single
pages are different decisions. It is on until an administrator turns it
off, as the issue asks for an organization that can disable the feature;
turned off, every link stops and none can be made, and turned on again the
links work again, since nothing was deleted. Making, revoking and the
switch go to the audit log.

The token is a secret in the address, so nothing through a link is kept by
a cache (`Cache-Control: no-store`, as Armature's), the api's answers and
nginx's reading view send no referrer, nginx writes neither address to its
log, and search engines are asked to stay away (`X-Robots-Tag` and the
`robots` element) unless the organization's #79 switch lets them in. Making
a link is no MCP tool: it opens a page to anybody outside, a decision for
the person in the share dialog, and the token is shown once, to them.

## 2026-10-06: Anybody reads an open space as nobody, through reads of their own

Public documentation (#79) is two switches. The organization's,
`org.anonymous_access`, is off until an administrator turns it on; while it
is off nothing is public, whatever a space allows. A space's is a grant of
view to the subject `anonymous` in `space_grant`, which the database takes
for view alone and never in a personal space, since that is named after its
owner and its pages are theirs. It is a grant rather than a column so that
the rule reads like every other space permission; it is kept out of the
permission grid, which names people and groups, and set on its own route by
whoever administers the space. Either switch changing goes to the audit
log. A second switch of the organization, off by default, says whether
search engines may list the public pages; until then every public answer
says `X-Robots-Tag: noindex, nofollow` and the reading view adds the same
`robots` meta element.

A request without a session knows no organization, so a public address
names it, `/public/{org}`, as Armature's share links and desk name theirs.
The api finds the organization by its slug, answers one that is archived,
closed or unknown alike with `404 not_public`, and from then on reads as
an anonymous reader: the transaction sets `app.anonymous` and names nobody.
Authentication leaves these paths alone, so a session riding along neither
widens nor refuses them. `perm_space_holds` gives that reader view of a
space the organization and the space both open and nothing else anywhere,
and `perm_page_viewable` adds that the page and every page above it are
published and the page is out of the trash; a view restriction, which
always names people, is never passed, so a restricted page is never
public. Every table carries a restrictive policy for the anonymous reader:
closed outright, or for `space`, `page` and `attachment` read only, and a
test asks every table for one, so a table added later is closed until
somebody decides otherwise. Straight through SQL as `stator_app` the
reader finds no person, membership, comment, reaction, view, version,
grant or audit entry, and every write is refused or reaches no row.

The member routes stay as they are, `401` without a session, and the
public reads are routes of their own with answers of their own: the
organization and its open spaces, a space's tree, a page, a file and a
search, all `GET`. A shape that has no field for a person cannot leak one
when somebody later adds a name to the member's shape. A page's body is
sent through `document.ForAnonymous`, which drops the person and label of
every mention, which the reader shows as "someone", the person a task
report asks about, and the threads' marks. The search index holds the
names a page mentions, so each public hit is matched again, and its
snippet cut, from the words without them; a page found only by a name is
not found.

What names somebody is left out rather than anonymised: authors and who
updated a page, the owner and the verification, which name a person by
their nature, comments, whose discussion is between members and whose
replies mean little without who wrote them, reactions, readers and the
counts by person, watchers, contributors, assignees, the history and
presence. An anonymous read is no view either, since a view is a person on
a day. The generated blocks (lists of pages, task reports, contributors,
calendars, Armature's issues, link cards and includes) are each reader's
own, asked as that reader, so the reading view says in words what they
list, as a comparison of versions does, and an include links to its page.
A link into the app leads to the public view of its page or space, which
says so when it is not public and offers signing in, and any other
address of the app leads to signing in, back to where it pointed.

A public answer is the same for everybody, so it may be kept by a shared
cache for a minute (`Cache-Control: public, max-age=60`); a page restricted
or a switch turned off is refused at once by the api and within that
minute by any cache in front of it. Drafts, the shared draft's socket,
MCP, tokens and every write remain for members, and an assistant reads as
the member it acts for, so no public read is a tool.

## 2026-10-05: A guest is a member held to one space, as a limited token is

A guest (#69) is a row of `org_member` with the role `guest` and the space
they were invited to, not a second kind of principal, as Armature keeps a
portal customer a member with a role. Signing in, sessions, removal and the
audit log then work for guests as for anybody, and every rule that already
asks about members asks about them. An administrator of the organization
invites one by address into a team space as a viewer, a commenter or an
editor; the account is made if there is none, the grants naming them are
written with the membership, and they sign in through the organization's
provider with that address. A personal space takes no guest. There is no
link mailed with a token: the provider proves the address, as it does for
everybody else.

What a guest reaches is what a token limited to their one space reaches, so
the functions that hold such a token (`perm_token_reaches`,
`perm_token_whole`) hold a guest too, for every caller who asks about them
and whether or not the request says so: no other space, no personal space,
no tokens, no organization grant beyond `use`, nothing the route table marks
`orgWide`, which answers `403 guest`. Within their space a guest holds only
the grants that name them: what everyone holds there is for the
organization's members, and a guest invited to read stays a reader in an
open space. The database refuses what would widen a guest: a grant in
another space, administering their own (whose permission table names the
organization's people and groups), a place in a group, which carries its
members into every space it is granted, and any change of their role or
space; the remedy for each is to remove them and invite or let them in
anew. The provider's groups neither promote a guest nor take them in, as
they never move an owner. Deleting the space removes its guests.

Of the organization's people a guest sees only themselves and the people
of their space: whoever a grant of the space names, directly or through a
group, and whoever published, commented on or owns a page there. That is
what the space shows by name, its authors, commenters and owners, and whom
a guest may sensibly mention. It is not everybody who may read the space,
which for an open space is the whole organization and would make the
mention picker the directory the guest may not see. `app_user` and
`org_member` hold a guest to it by policy, so the people picker, the mention
picker, the search's author filter and every name joined into a page agree;
groups, join requests and the provider settings are hidden from a guest
outright. A guest's mention of anybody else is dropped before it is told,
and refused by the outbox's policy if a request names them anyway. The
members list is the administrators', and marks each guest with their space.

The guest's interface follows from `organization.guestSpace` in
`/auth/me`: they land in their space, and the navigation offers no space
directory, hub, tokens or personal space.

## 2026-10-05: Without Valkey, api processes pass shared drafts' changes through Postgres

Editing together (#65) first needed Valkey for more than one api pod, so the
chart refused several pods without it. Every deployment has Postgres, and
its LISTEN and NOTIFY carry small messages between every process connected
to the primary, so without Valkey the api now uses that instead, chosen at
startup from the same `STATOR_VALKEY_URL` that turns Valkey on. Each
process holds one connection of its own to the primary, which it listens
on and opens again, with a backoff, when it is lost.

A notification holds at most 8000 bytes, while an update may hold 4 MB. An
update is a row in `page_collab_update` already, so the bus carries only a
pointer to it (page, room, row number), sent with `pg_notify` inside the
transaction that inserts the row: Postgres delivers it on commit, so no
receiver hears of a row it cannot read yet, and none of a row rolled back.
The receiver reads the row as one of the people it holds in that room, so
row level security decides as it does for every read. Awareness and the
small control frames go whole when they fit under
`collab.MaxNotifyBytes`; awareness that does not is dropped, since browsers
renew theirs, and anything else that does not fit sends the room's
browsers on the other processes to load it afresh, so nothing is lost.

Either bus can lose messages while a process cannot hear it. Each process
therefore remembers, per page it holds connections to, the last update the
bus brought; once its listener (Postgres) or subscription (Valkey) is back,
it reads each such room as somebody in it and relays every update past that
number, and the room's base, or sends its browsers to load afresh when the
room was thrown away meanwhile. A lost frame no longer waits for its
browser's next load. Updates are numbered by an identity, so one numbered
lower can commit after one numbered higher; in the instant around a lost
connection catching up can miss it, and its browsers get it at their next
load.

During a rolling update that adds or removes Valkey, pods on different
buses do not hear each other, so people on them see each other's edits
when they reconnect rather than live. Nothing is lost, since every edit is
stored before it is passed on.

## 2026-10-05: Editing together is a Yjs document the api stores and relays without reading

People who open a page's editor at once (#65) edit one shared draft, a Yjs
document: the body is its XML fragment, bound to the editor by Tiptap's
collaboration extensions, and the title a text of its own, changed by its
common start and end so two people retitling at once both keep their part.
Yjs merges concurrent and offline changes without conflicts, which the
editor's per-person drafts never could, and it is what Tiptap supports.

The api never reads the document. There is no Go implementation of Yjs as
solid as the JavaScript one, and a server that only stores and relays
cannot get the merge wrong. `GET /pages/{id}/collab` is a WebSocket in
y-protocols' framing; each update a browser sends is appended to
`page_collab_update` and passed to everybody else in the room, and an
opening browser is sent every stored update. Since the server knows no
state vector, the browser works out what the server lacks from the updates
it was sent (`Y.encodeStateVectorFromUpdate`) and sends just that, which is
how a browser that was offline merges what it wrote. Many updates are
merged by a browser too: a load of 200 or more asks for `Y.mergeUpdates` of
exactly the rows it carried, and the database swaps them for the merge only
if they are all still there. The browser keeps each room in IndexedDB, so
words written offline outlive a closed tab.

Who may join is decided as for any edit: a signed-in session (a token has
no use for a socket, and a read-only one would be writing), from this
site's origins (the handshake is a GET, so `sameSite` holds it like a
write), for a page the person may edit. The connection asks again every 30
seconds, with the person's own credential and the page's rules, and closes
with 4401 or 4403 when the answer changed; and every update is an insert
the database's policy holds to `perm_page_editable`, so a revoked editor is
refused on their next keystroke even between checks. The tables carry
row level security like every other, and an update names its sender by a
trigger.

A room is one life of the shared draft, with an id of its own. The first
person in is asked to seed it, from their own draft when they have one or
else the page, with the version that came from, which becomes the room's
base; nobody else's editor appears until the seed has arrived, so nothing
is typed into an empty room and doubled later. A publish from the room
moves its base on (the publisher says so, and the database checks they
published that version), so the next person's publish is not refused as a
conflict. When the page was published from elsewhere and the room holds
nothing past its last publish, the next opener starts a new room from the
page; a room with unpublished changes is kept, and its publish meets the
usual conflict. Discarding throws the room away for everybody, who reload
into a new one seeded from the published page.

Drafts and publishing stay as they were. Each person's own draft is still
what publishing publishes: the editor saves the shared draft into it after
the person's own changes and before a publish, so the existing publish,
conflict and comparison work unchanged, and an editor that cannot reach the
socket in six seconds edits alone, saving its draft as before. The person
who seeds a room from their draft does not lose it, and somebody joining a
room overwrites their own draft with the shared one on their next change.

Several api processes reach one room through a bus, each process
listening before it serves: Valkey's publish and subscribe, one channel per
page, or without Valkey Postgres's LISTEN and NOTIFY (see the next entry).
Limits are constants of
`internal/collab`: a message of 4 MB, which the database holds an update
to as well, 50 connections per page per process, a ping every 25 seconds
to keep proxies from closing a quiet socket, and a slow browser is let go
to load again rather than buffered without end.
## 2026-10-05: Office documents are converted to PDF once, by a service of their own

A PDF and an office document (docx, xlsx, pptx, odt, ods, odp and the older
doc, xls and ppt) are previewed as a PDF in the browser's own viewer, so
Stator ships no viewer of its own and every kind looks the same.
`GET /attachments/{id}/preview` answers a PDF with its bytes and an office
document with its conversion; `preview` on each file says which applies, and
`none` when the site cannot convert it.

The conversion is a headless office suite in a container of its own, behind
a small HTTP API (Gotenberg, MIT licensed), which the api calls at
`STATOR_CONVERTER_URL` and nothing else reaches. Office suites are large and
read untrusted documents, so the suite runs apart from the api, with its
browser half off; it is the operator's service, like the bucket, so the call
does not pass the outbound guard. Without the setting office documents have
no preview and PDFs still do.

The first reader of a version waits for its conversion, and the PDF is kept in
the bucket beside the file, recorded in `attachment_preview` keyed by the
file's row, which is one version: a new version is converted afresh, nothing
is converted twice. A document the suite refuses is recorded as failed and
not tried again; a converter that does not answer is not recorded, so the
next reader tries again. Two readers at once may both convert; the first to
commit keeps the row. A preview is its file's: whoever may see the file sees
it and may be the one whose visit makes it, the database holds the row to
that and to its organization, and it goes with the file by a cascade that
leaves a tombstone like the file's. Documents over 20 MB are not converted,
and a conversion gives up after 20 seconds, inside the request timeout, so
the reader is told in a sentence rather than cut off.

The browser fetches the PDF and shows it in a frame from a blob typed as a
PDF, rather than framing the API: a refusal then reads as a sentence in the
dialog instead of an error body in the frame, and the API keeps refusing to
be framed at all. The app's policy allows `blob:` frames for this alone.
## 2026-10-05: A space template is data the space's creation reads, in one transaction

Space templates (#64) are built in, like the page templates: one JSON file,
`backend/internal/template/builtin/spaces.en.json`, compiled into the api
and served by `GET /space-templates`. Each holds the home page's body, the
pages below it with their bodies, labels and children, and the permissions
everyone gets. `POST /spaces` takes a template's key and `space.Create`
writes the whole space from it in the transaction that makes the space, so
a space never exists with half its pages, labels or grants; an integration
test refuses the last label from inside the database and finds nothing
left. The audit entry is the one a blank space gets, with the template's
key added, rather than an entry per page: an administrator made one space.

The pages are written the way the home page always was, straight into
`page` and `page_version` as version 1, rather than through the page
service's publish. Publishing settles tasks, notifies mentioned people,
emits events for watchers and syncs Armature links; a new space has none
of those to settle, and the page service imports the space package, so the
space package could not call it without a cycle. The unit test holds every
body to what this path can take: the allowlist, no task lists, mentions,
inline threads or hints. A hint would be stripped on the way in, as from any
published page, which is also why the space templates write their own short
bodies rather than reuse the page templates. A list of pages by label or of
recent updates in a body names no space and is given the new one's key, so
it lists that space's pages rather than every space's.

Permissions are a preset for everyone in the organization, the only
subject a built-in can name: a knowledge base lets everyone add pages and
comment, a team space lets everyone read and comment while the team's group
is added by hand, and documentation is read by everyone and written by
whoever its administrators name. The creator keeps the administer grant the
database gives every new space, so the creator can widen or narrow any of
it. A personal space takes no template: its permissions are its owner's
alone by design. The text is English, like the page templates', and will be
translated the same way, a file per language.
## 2026-10-05: A template button names its template by key, and contributors are read from the history

A template button (#62) holds a template's key, where its page goes and its
words, never a copy of the template: a template that changes later makes
the next page as it now reads, and the key is what `GET /templates` serves,
so a template that later answers to a key is found the same way. The place
is a page by id, which a move does not lose, or the top of a space, which
is the space's home page; a button that names neither puts its page at the
top of the space it is in. The page is made by
`POST /templates/{key}/pages`, the server reading the template, as an
unpublished page of whoever clicked, as a new page from the tree is, so
the click checks the same rule as the tree and the database's insert
policy holds it too. The title is the button's pattern, else the
template's, else its name; the browser fills `{date}` with the reader's own
day, as the new page dialog does, and the server fills any left with today
in UTC. Each reader's view asks `GET /template-button` whether they may add
a page there, so one who may not sees the button disabled with a sentence
rather than a click that fails.

A contributors block counts published versions, which the history already
names to every reader of the page, rather than drafts or the audit log,
which name work nobody published or are for administrators. It counts the
page alone or with the pages below it the reader may view, read in one
query as that reader, so a restricted page below hides its versions as it
hides itself, and the version policy keeps them from the reader in SQL
too. The people come by the most versions, then the latest; somebody whose
account is gone is left out, as their versions name nobody.

## 2026-10-04: A chart from a table holds its table

A chart from a table is a block whose one child is the table, not a chart
that points at a table elsewhere on the page. A pointer would need an id on
every table and would break when the table is deleted or copied without
it; holding the table makes "the chart follows the table" true by
construction, keeps the table searchable and exported as an ordinary
Markdown table, and lets the author remove the chart and keep the table.
The numbers are read in the browser on every draw rather than stored, so
there is nothing to fall out of step. Markdown has no chart, so the export
writes a marker before the table that the import joins back to it.
## 2026-10-05: A calendar is rows of its space, and a page draws a month of it

Team calendars (#60) are kept beside the pages rather than in them: an event
is changed far more often than a page is published, by people who are not
editing that page, and a page showing a calendar should not gain a version
for every absence. A space keeps calendars, each a name unique in it, and
their events are rows of `calendar_event`, read by everybody who reads the
space and kept by whoever may add pages to it, the same people who keep its
pages. An archived space freezes them, as it does its pages. The policies
call `perm_space_holds` and `calendar_writable`, so a statement as
`stator_app` that forgets whom it is for changes nothing; a calendar never
moves to another space, nor an event to another calendar, since the app may
write neither column, and the author of each is stamped by a trigger.

An event lasts whole days or runs between two instants. Whole days are kept
as midnights in UTC, the last day included, as a date node is read in UTC,
so an absence falls on the same days for every reader; a meeting is kept as
instants and shown in each reader's own zone. A month is asked for by the
reader's own first midnights, and an event that lasts all day counts its last
day whole, so a reader east or west of UTC still gets every day they see.

The calendar block holds which calendar and, if any, which Armature project;
never the events. Each reader's view asks for the month and, with their own
token, Armature's `GET /projects/{key}/calendar`, drawing each dated issue
on its due day; Armature's sprints, milestones and versions are left out,
being a project's plan rather than a team's days. Without a token the events
still show, with a sentence asking the reader to connect. The month is laid
out in weeks from Monday, as ISO weeks are; on a phone it becomes a list of
the days that hold something.

## 2026-10-03: A file's versions are uploads of one name to one page

A file has versions so the files block can say which one a reader sees. An
upload under a name the page already has, compared without case as people
read names, is that name's next version. Each version stays its own row with
its own bytes, so a link or picture in an older page version still shows
what it showed; nothing is overwritten. The number is stamped by a trigger
under a lock on the name, so two uploads at once cannot take one number and
the app cannot claim one. Deleting a version leaves the others their
numbers. A copy of a page is a new page, so its versions count from 1 again
in the order they were uploaded.

## 2026-10-03: A task report filters by relative days and by the reader

A task report stores its filter in the page, as the lists of pages do, and
asks for the tasks each time it is read. Its due day choices are relative
(overdue, today, the next 7 days) rather than fixed dates, because a report
on a status page is meant to stay true without editing, and they are judged
by the database's `task_today()` in UTC, the same day the reminders use. The
assignee may be "whoever reads the page", stored as `me`, so one page shows
each person their own tasks; a named person is stored by id and named by the
report's answer, so a renamed person reads right, and one who left, whose
tasks fall unassigned, leaves an empty report rather than a broken one.

## 2026-10-03: A list of pages holds what to list, and asks each time it is read

Content by label and recently updated store their settings in the page, as
the chart and the report do, and each reader's view asks for the pages: an
overview stays current without anybody editing it, and two readers of one
page rightly see two lists when they may read different pages. Both count a
page from when it was last published, which only publishing moves, so a
draft or a move does not bring a page to the top. Folders, the
trash and the archive stay out, as they do on the home page. A latest blog
posts block needs blog posts first, so it moved to #72.

## 2026-10-03: Page properties live in the page body and are read when asked

A page's properties are a block of its body, a row per name with the value
as inline content, not columns of the page or a table of their own. They are
versioned, compared, exported and searched with the rest of the page, take
the same mentions, dates and statuses as any line, and need no second place
to keep in step. The name is an attribute of its row, so a value is one run
of inline content the editor already knows how to edit.

A properties report reads the published bodies of the pages carrying every
label given, at most 200, as the decision log reads decision items: on each
request, as the reader, so restricted pages and drafts stay out without a
copy to keep current. Names are matched by their words in any case, the
first value of a name on a page wins, and a row without a name is one still
being typed, left out of the report and the read view alike. A report takes
the labels as AND, so a register narrows by adding a label.

## 2026-10-03: A roadmap is Armature's plan, grouped by Stator

A roadmap block holds a project, a query and a grouping, never the dates,
as a chart holds what to count. Each reader's view asks Armature's
`GET /projects/{key}/plan` as that reader. The plan is the project's issues
as a tree, with each issue's start and due dates or, for an epic without its
own, the span of its children's; it is returned whole, with the keys the
query matches. Stator groups only the matched issues, so an epic the query
leaves out still heads the issues it matched, and the epic's span is
Armature's, not one Stator works out again.

Grouping by epic uses the nearest epic above an issue; an initiative above
the epics is a row like any issue outside an epic, not a group, so a roadmap
of epics is not one group holding everything. Grouping by team uses the
issue's own team. Issues with neither date are counted under the timeline
rather than drawn at an invented day, and a block stops at 100 rows and says
how many it left out.

The timeline is rows of labels beside tracks of whole days, laid out in HTML
rather than one drawing, so it reflows on a phone, label above track. A bar
takes its status category's board colour, as the board does, with a legend
naming each, and every bar's dates are also in words for screen readers. An
epic's span taken from its issues is an outline, its own dates a fill.

## 2026-10-03: Armature counts a chart, with the reader's own token

A chart block holds what to count and how to draw it, never the counts, as
the issue list holds its query and never its rows. Each reader's view asks
Armature's reports, `chart` for a pie and `created_vs_resolved` for the
other, as that reader: Armature counts every issue the query matches that
the reader may see, which no page of search results could, and two readers
of one page may rightly see two different charts. The reports take a
project, so a chart names one; its query narrows within it.

The pie is a donut, as Armature draws it, with every share also in a table
beside it, so no number hangs on telling colours apart. A status category
keeps its board colour; any other field takes the chart palette's slots in
order, a fixed set of eight checked for colour blindness against the light
and the dark surface, and past eight the smallest shares fold into Other
rather than take a colour nobody can tell apart. Created against resolved is
two lines on one axis of whole issues, read a day at a time with the pointer
or the arrow keys and offered as a table. The drawing is one unit to the
pixel of the page it is on, so its words keep their size on a phone.

## 2026-10-03: How a page looks is a property of the page, not a version of it

A page's emoji, width and cover are columns of the page row, changed at
once with `PUT /pages/{id}/appearance`, not part of the body or the
versions: they say how the page is presented, not what it says, and an
author choosing a cover should not have to publish to see it or find it in
the history between two edits of the words. Changing them needs edit on the
page, as editing the words does; a trigger holds the app's role to that.

The emoji is one emoji, perhaps several code points joined, never words:
the service checks it against what makes an emoji, and the database keeps
it to sixteen code points without spaces. The tree shows it in place of
the folder mark when there is one, so a page is found by sight.

A cover is one of the page's own pictures: the foreign key on
`(id, cover_attachment_id)` to `attachment (page_id, id)` refuses any other
file, and deleting the file takes the cover with it, as the hub goes with
its page. Its focus is a point in percent of the picture, kept in view by
`object-position` however wide the window cuts it; the dialog sets it with
a click on the picture or with the arrow keys.

Full width takes the page's reading width limit off, in the reader and the
editor alike, for text as well as wide blocks (see the entry of 2026-10-06
on the measure).

## 2026-10-02: An include is read for each reader, and the chain it sits in catches cycles

An include is one block, `include`, holding a `pageId` and, for one
excerpt, an `excerptId`; never the words. The words are read when the page
is shown, through `GET /pages/{id}/included`, as the reader: so the included
page's restrictions hold for every page that includes it, and its next
version reaches them all without anybody saving them. It shows the
published body only, as the included page's own readers see it.

Whatever keeps the words from a reader, a restriction, a page never
published, a folder, an excerpt removed or a page deleted, answers the same
404 and the same notice, so an include tells a reader nothing about a page
they may not read, not even that it exists.

Cycles are caught twice. Saving a page that includes itself, at any depth
of its body, is refused, as a draft and as a version. A longer loop, A
includes B includes A, cannot be refused on save without reading pages the
author may not read, so it is caught as it is shown: each include asks with
`via`, the chain of pages it sits in, starting from the page being read,
and the server answers 409 for a page already on it, or for a chain five
deep. The reader then sees the loop's notice once, in place of the page
showing itself inside itself.

Included words take no anchors and no inline threads of the page they are
shown in: those belong to the page whose words they are. They are not in
the including page's search text either, for the same reason; search finds
them on their own page. The Markdown export writes what the include points
at, which an import into the same organization reads back.

## 2026-10-02: An excerpt is a frame in the page, found by an id that outlives its name

An excerpt is one block, `excerpt`, around the blocks it names, with an
`id` and a `name`. It is part of the page rather than a record beside it,
so it is versioned, restricted, searched and exported with the page, and
moving text in or out of it is ordinary editing. An include finds it by its
id, so renaming an excerpt breaks nothing; the name is what a picker shows.

Within a page ids and names are unique, names ignoring case, and an
excerpt never holds another at any depth: an include of the outer one would
otherwise carry the inner one twice over. The validator refuses all three
straight from the body, and the editor keeps to them as it goes: a pasted
copy gets a new id and a stock name, and an excerpt pasted into another
gives up its frame and keeps its blocks. The name box may be emptied while
a name is typed; the page keeps the last name until there is a new one.

`GET /pages/{id}/excerpts` reads the published body a reader may view, so a
draft's excerpts are its author's until it is published, as an include
shows published words only. The picker chooses a space, a page from its
outline, and the whole page or one excerpt, leaving out the page being
edited; the include block (#53) is what uses it.

## 2026-10-02: A link card keeps its address, and the server reads the page for each reader

A link card is one block, `linkCard`, holding an http or https `url` and a
`view` of `card` or `embed`. What the linked page says, its title, summary
and site, is not stored: it would be readable by anybody who reads the page
whatever the site later says, and would go stale, as an Armature issue's
summary would. The inline view is no node of its own: it is an ordinary link
whose text is the page's title, so it reads, exports and searches as any
link does.

`GET /link-preview` reads the page on the server, not in the reader's
browser, so a reader's address and cookies never reach the site and the
browser's policy stays `connect-src 'self'`. The read goes through the same
outbound guard as webhooks and Armature, `STATOR_OUTBOUND_ALLOW` included,
so a member cannot make the server read Valkey, Postgres or a cloud metadata
address. It takes only text/html, reads at most 512 KB looking for the head,
gives up after 5 seconds and three redirects, and sends no credentials.
Answers are kept in Valkey for an hour, an unreadable page for five
minutes, shared by every organization: what a public page says about itself
is the same for all of them. A page that cannot be read still gets a card,
named by its host. No picture from the page is shown: it would load from the
site in the reader's browser, which the policy refuses and the reader did
not ask for.

Embeds are an allowlist in code, not a setting: YouTube through its privacy
enhanced player, Vimeo's player and Figma's embed page, each worked out from
the address alone, without reading the site. The Content-Security-Policy's
`frame-src` names exactly their origins, a unit test holds the compose and
chart policies to the list, and the frame is sandboxed to scripts, its own
origin, popups and presentation. A card whose site has no player shows as a
card whatever its view says.

An address pasted alone on an empty line becomes a card, and the site's
player when it has one, without an undo step of its own for the change of
view; pasted among words it stays a link. The compose stack lets the guard
read the web container, so the browser suite has a page inside the network
to preview; the chart allows nothing inside the network, as before.

## 2026-10-02: A diagram is stored as its Mermaid text and drawn in the reader's browser

A diagram is one block, `diagram`, holding its Mermaid text in `source`, up
to 20000 characters. The text is what is versioned, compared, searched and
exported, so it is all that is kept; the SVG is drawn from it wherever the
page is shown. Drawing on the server would take a browser engine in the API
for every save, and a stored drawing would be markup in the body that goes
stale when Mermaid or the theme changes.

The editor shows the text in a field with the drawing below it, drawn again
once typing has paused for 300 ms; a text Mermaid cannot read keeps the last
drawing's place with the reason and the text. The reader's view draws it
the same way and offers the drawing as an SVG file to download, which is the
export of the drawing; the Markdown export writes a `mermaid` fence, drawn
by Markdown readers that draw them and read as text by the rest, and the
import reads one back as a diagram.

Mermaid runs in strict mode with labels drawn as SVG text: click handlers
and scripts are dropped, and a tag in a label reads as its own words rather
than becoming an element, since strict mode alone keeps an `<img>` that would
fetch its source. It is capped at the text limit and 500 edges. It takes its
colours from the theme's tokens when they are hex, and Mermaid's light or
dark set otherwise. It is large, so it loads with the first diagram on a
page rather than with the application.

Search reads a diagram's text as lines of the page, in `document.PlainText`
and in `page_plain_blocks` alike (migration 00380).

## 2026-10-02: A formula is stored as its TeX source and typeset by each reader's browser

A formula is a node holding nothing but its LaTeX source: `mathInline` in
a line of text, `mathBlock` on a line of its own, each with one `latex`
attribute of up to 4000 characters. The source is what an author edits and
what search, an export and a copy read, so it is the one thing kept; the
typeset markup is drawn from it with KaTeX wherever the page is shown,
in the editor and in the read-only view alike, and never stored, so no
body carries markup a reader's browser would run.

KaTeX runs with `trust` off: `\href`, `\url`, `\includegraphics` and the
`\html...` commands draw as their own names in red rather than as links,
images or attributes. Expansion stops at 1000 macro steps and a box at 20
em, so a formula that calls itself or asks for a huge box cannot hold or
cover the page. KaTeX writes MathML beside its markup for screen readers.
Its stylesheet and fonts are bundled, as the text fonts are, so a formula
needs no third party at load time.

The dialog that edits a formula refuses a source KaTeX cannot read, with
KaTeX's own reason; the server takes any source within the limit, since
TeX's grammar is KaTeX's to judge, and a reader meets a broken formula as
its source in red, saying so.

Search reads a formula by its source, in `document.PlainText` and in
`page_plain_blocks` alike (migration 00370). A Markdown export writes an
inline formula between dollar signs and a block one as a `math` fence,
which is how Markdown that typesets formulas writes them; the import reads
the fence back as a formula. An inline formula comes back as text, since
reading dollar signs as formulas would turn prices in imported prose into
mathematics.

## 2026-10-02: A decision is a line of the page, and the log is read from published bodies

A decision item is one node, `decision`, holding a line of text as a
paragraph does and a `state` of `decided` or `undecided`. It is a block of
the page rather than a record beside it, so it is written, versioned,
restricted, searched and moved with the page it is on, and a decision can
sit in a panel, an expand block or a table like any line. Its state is a
label in words before the line, so it reads without colour, and in the
editor that label is the button that changes it.

A space's decision log, `GET /spaces/{key}/decisions`, is read from the
published bodies of the space's pages the reader may view, out of the trash
and the archive, newest page first and in reading order within a page; the
database finds the pages with a JSON path, and the service quotes the
lines. Nothing is kept beside the pages, so the log cannot drift from them:
a decision changed in a draft reaches the log when it is published, and a
restricted page keeps its decisions to those who may read it. It is cut at
500 decisions, saying so, and filters by state.

Search reads a decision as a line of the page's words, in
`document.PlainText` and in `page_plain_blocks` alike (migration 00360).
Markdown has no decision items, so an export writes one as a line that
begins with its state in bold.

## 2026-10-02: The hub is a page an administrator points at, and landing on it is a redirect

The organization's hub is not a document of its own but one of its pages,
named by `org.hub_page_id`: a page is already written, published, restricted,
watched and kept in history, and a hub that needed its own editor and its
own rules would repeat all of that. Any page of any space may be the hub;
the foreign key on `(id, hub_page_id)` to `page (org_id, id)` keeps it one of
the organization's own, and a page deleted for good stops being it.

Only administrators choose it, and the database holds to that: a trigger
refuses a change of the hub or of landing by anybody else acting through
the app's role. Everybody else reads it through `GET /org/hub`, which names
the page only to whoever may view it, out of the trash and the archive. A
hub the reader may not read is no hub for them: the navigation leaves it
out and they land on their own home.

`hub_landing` makes `/` a redirect to the hub, decided in the route before
anything renders, so nobody sees their own home flash first. The reader's
own home then lives at `/home`, where the navigation's Home leads; a check
keeps landing off while there is no hub.
## 2026-10-02: A folder is a page of another kind, version 1 from the start

A folder is a row of `page` with `kind = 'folder'`, not a table of its own:
it has a parent, a rank, a place in the trash and the archive, restrictions
that reach what is below it, and moves and copies with its subtree, all as
the page tree already does them. What it lacks is everything a page holds:
a CHECK keeps its body the empty document, and one trigger on each table of
a page's content (versions, drafts, files, labels, threads, comments,
reactions, shares, owners, verifications) refuses a row for a folder,
whichever service writes it. The API answers that refusal as a 409 with the
code `folder`. A row stays the kind it was made as, and a home page is
never a folder, since a space opens on it.

A folder is made at version 1 with no version row. A row at version 0 is
its creator's alone until published, and so would be everything put in it,
but a folder has nothing to publish. It has no history, so renaming it
changes its title and nothing else. Its `published_at` stays empty, which
keeps it out of the home page's feeds and the stale report; search finds
pages by their published version, which a folder has none of.

Opening a folder shows the pages and folders in it, the list a child pages
block draws, and offers new pages, new folders, renaming, moving and the
trash. It has no editor, history, comments or watching of its own.
## 2026-10-02: A personal space is an ordinary space with an owner, and starts closed

A personal space is a row of `space` with `owner_id` set to the person it
belongs to, not a kind of space of its own: pages, search, trash, archive and
permissions work in it as in any other, and sharing it is the space's own
permission table, which its owner administers. A partial unique index on
`(org_id, owner_id)` keeps it to one each, so the directory and the button
that offers one can trust there is at most one.

Making a space takes `createSpace`, which members do not hold by default, but
a personal space takes only `use`, with a token for the whole organization:
everybody needs somewhere to draft before sharing, and it reaches nobody
else until they share it. The insert policy says so, and requires the owner
to be the person making it, so nobody makes one in somebody else's name. The
grant trigger gives a personal space no `everyone` rows, only its owner's
`administer`.

The owner is fixed when the space is made; a trigger refuses any other
owner, since a space handed over would be somebody's without their asking.
When the owner's account goes, `owner_id` becomes null and what is left is an
ordinary space that only administrators reach.

Administrators of the organization still reach every personal space, as they
reach every other space: they hold every permission so that no space is ever
orphaned (2026-09-30), and a space nobody else can open is the one most at
risk of that. Private means private from the other members.

## 2026-10-02: A shortcut is a ranked row of the space, read through the page's own view rule

Space shortcuts (#39) are links the administrators of a space pin above its
page tree. Armature's project sidebar has none, so there was nothing to
follow. A shortcut is a row of `space_shortcut` naming either a page or an
address, never both, with a label and a rank from `internal/rank`, the
ranks sibling pages use: moving one writes that one row, and the move names
the shortcut it goes after, as a page's place does, rather than resending
the whole order, which a list a reader sees only part of could not do.
They are kept under Shortcuts in the space's settings, with move up and
move down buttons that the keyboard works as it works everything else;
dragging was left out, since the tree's own drag has the place dialog
beside it for the keyboard and a list of thirty needs no more than buttons.

Who keeps them is who administers the space, as for its name and its
archive: they are what everybody reading the space sees first, so one
editor should not rearrange them for all. Changes are allowed in an
archived space, as renaming it is, since a shortcut is the space's
furniture and not a page. Each addition, move and removal is written to the
audit log on the space, as changes of its details are. Reading them is the `list_space_shortcuts` tool;
changing them is administration, and not a tool.

A shortcut to a page must never name a page to somebody who may not view
it, so the list is read through `perm_page_viewable` for the reader, in the
query and again in a restrictive policy on the table: a restricted page's
shortcut is simply not in the list, and nothing says one was left out. A
shortcut to a page in the trash is left out for everybody and comes back
with the page; a purge takes it along. An archived page stays readable, and
its shortcut is shown, marked archived, rather than hidden as the tree
hides it: an administrator pinned it on purpose and may unpin it. The
database lets an administrator point one only at a page they may view, out
of the trash, and lets nobody change where a shortcut points or which space
holds it, only its rank and label.

An address is held to the web's own schemes, `http` and `https`, with a
host and no name or password before it, by the API and by a check on the
table, so `javascript:`, `data:` and the like are refused whatever writes
the row; the client opens one only when it still parses as such, in a new
tab with `noopener noreferrer nofollow`, as links in pages are. An address
without a label shows its host, and a page without one shows its title as
it is now, so a rename needs no second change. A space holds at most 30,
`shortcut.MaxPerSpace`, which `space_shortcut_max()` repeats in SQL and a
trigger counts under a lock per space, so two additions at once cannot both
pass.
## 2026-10-02: A task is a checklist item of the published page, assigned by its first mention

Tasks (#56) add no node. A checklist item is a task; the first person its
own words mention is its assignee and the first date in them its due day,
while an item nested in it is a task of its own. Mentions and dates already
read, search, diff and convert to Markdown, the editor already offers both
behind `@` and `/date`, and a person reads at a glance who has a task and
by when without learning a new block.

The document is the only record anybody writes. On every path that
publishes (publish, restore, a page made published, an update, Markdown)
and on a copy, the page's `page_task` rows are written from the body the
database stored, so nothing a client sends besides the page itself says
what the tasks are. Each item gets a `taskId`, given by the server at
publish as inline threads are settled there, and the editor keeps it; an
id is never read from pasted markup nor kept on Enter, and an item that
claims an id an earlier item holds gets a new one. An item without an id
takes the id of a task of the version before with the same words that no
item claims, so a page written back as Markdown keeps its tasks and does
not tell everybody again.

An assignee must be a member who may view the page when they are assigned:
a publish that assigns a member who may not is refused with a sentence
naming them, and the database refuses the row too. A mention of somebody
who is no member assigns nobody, as it tells nobody. A copy, which nobody
wrote the names of, leaves such a task unassigned instead of refusing the
copy. Somebody who later loses access keeps the task, as an owner is kept,
and the list and the notifications, which ask `perm_page_viewable` when
they are read and delivered, leave it out until they may view the page
again; somebody who leaves the organization leaves it unassigned.

`stator_app` may write a page's rows only as somebody who may edit the
published page out of the trash (`page_stewardable`), and may not name the
columns that say who assigned a task, in which version, when it was done
and whether the reminder went: a trigger stamps them. The assignment
notification reads the rows assigned in the version its event names, so a
task given to somebody else before the worker came round tells only the
new assignee, and a republish tells nobody. The new kind `assigned` comes
first among the kinds, so a person assigned and mentioned in one publish
hears once, as assigned, as Armature tells an assignee. The reminder is
the worker's, as a lapse of verification is: `task.DueWatch` looks every
`STATOR_TASK_DUE_CHECK_INTERVAL` for open tasks whose day came, in UTC as a
date node is read, notes each once per day and assignee and writes
`task.due` for the kind `due`. A day already past when it is set is not
reminded, since the assignment says it.

Ticking a box changes the page, so it publishes the next version with
"Ticked off a task" and the task's words, for whoever may edit the page
and only on a published page out of the archive, without a notice to its
watchers. A done flag kept beside the document would have been a second
truth, and a reader who may not edit would change a page they may not
edit. A draft begun before the tick becomes a conflict as after any other
publish.

My tasks lists the caller's open tasks by their day and those without one
last, by keyset on `(day, id)` with no day as 9999-12-31, and their done
ones the latest first; the trash and the archive leave both. Each row says
whether the caller may tick it. Overdue and due today are words on a theme
tint, as Armature marks an overdue milestone, held to AA by the contrast
test. The rows carry space, assignee, day and state, which is what a report
of tasks (#57) filters by; it is not built here. Listing one's tasks is a
read tool for assistants and ticking one a page write like `update_page`.

## 2026-10-02: A token limited to spaces is limited by the database

A personal access token may name the spaces it reaches when it is made
(#111), as an Armature key names projects: `spaces` on the create request,
keys the maker can already see, and none for a token that reaches every
space its owner does. Inside its spaces the token does what its owner may,
an organization administrator's role included; outside them it finds
nothing, as if the spaces did not exist. Like Armature it is refused what
concerns the organization as a whole: making spaces, the audit log,
members, groups' roles, the organization's permissions, tokens, webhooks
and the Armature connection. The route table marks those operations
`orgWide`, the router refuses them with `spaces_token`, a unit test holds
the two to each other, and the MCP endpoint does not offer their tools.
What is the person's own and in no space (their profile, theme,
notifications and Armature account, and the people directory) stays open.

Armature narrows a key in Go, in the permission set its handlers read.
Here every rule is also a database function that the row level security
policies call, so the limit lives there too: authentication reads the
token's spaces, the transaction carries them in `app.token_spaces` beside
`app.user_id`, and `perm_space_holds` refuses a space outside them while
`perm_is_admin` and `perm_global_holds` refuse everything but `use`. Search,
the home feed, the stale report, watching, notifications and the audit log
all ask those functions, so none needed its own filter, and a query that
forgets the limit in Go is still held to it. The limit is the caller's
alone: a rule asked about somebody else, as the access inspector and the
notification workers ask, answers for that person, and naming another
person on a context drops it.

A token keeps `spaces_only` apart from its rows in `api_token_space`, so one
whose spaces are all deleted reaches nothing rather than everything; it
lists `allSpaces: false` with no keys, which is why the answer carries
`allSpaces` where Armature's empty `projects` alone means every project.
The spaces are fixed when the token is made: rows may only be added in the
transaction that made it, `spaces_only` cannot change, and a limited token
can neither read nor write tokens at all. Listing a token names only the
spaces its reader may still see.

Two kinds of a person's own rows name a page without asking whether they
may view it: their page views and the shares they sent. Their policies
also ask `perm_token_reaches_page`, so a limited token reads only those
about pages in its spaces, while a session keeps reading its own rows as
before. The page view counts and readers ask `perm_page_viewable` and
`perm_space_holds`, and follow the limit with no change.

## 2026-10-02: A view is a person on a day, counted for every reader and named only to editors

Page views (#97) answer how often a page is read and by how many people.
Armature counts no views, so there was nothing to follow. A view is one
person opening the page on one day, in UTC: a row of `page_view` per
person, page and day, written with the visit the page already posts and
never again that day, `INSERT ... ON CONFLICT DO NOTHING`, which leaves the
row untouched. A count of every load would have cost a write per load and
grown with reloads and open tabs, which say nothing about how much a page
matters; one row per person and day costs at most one write per reader and
day, and is what makes "unique readers" a count of rows. The visit kept for
recent pages and the stale report used to rewrite its row on every load;
it now leaves a row alone that is already the person's latest and from
today, so a reload writes nothing at all (`TestAReloadWritesNothing`
watches the rows' versions). Opening a page from the stale report still
posts no visit, and so counts no view.

Everybody who may view a page reads its counts: views and readers in all,
and over the last 30 days. They say how many, never who, and a page one
may not view answers 404 as everything about it does. Who read it is for
the people who may change it, its editors and the space's administrators,
as a page's restrictions are: they are the authors the issue asks for, and
a name list is a different thing to hand every reader. An archived page
still lists its readers, since listing changes nothing. Each person
chooses in their profile whether their name appears there
(`show_in_readers`, on by default, for every organization they are in);
hidden, they are counted and the list says how many chose not to be named.
Listing by default keeps the list useful to the author, the choice is a
switch away, and the names are bounded twice: to the editors and to the
retention.

The rows naming people are kept for `STATOR_RETAIN_PAGE_VIEWS`, 90 days by
default, at least 30 so the recent counts always have their whole period,
0 to keep them. The worker prunes once a day, as for the audit log, as
`stator_admin` through `page_view_prune`, which refuses a younger cutoff
whatever it is handed and adds what it takes to `page_view_tally`, one
anonymous number per page, so a page's total survives the names. Somebody
who leaves the organization leaves their rows without a name
(`ON DELETE SET NULL`), still counted as views. All-time readers are the
members with a visit to the page, which has no retention but names nobody
to anybody else.

The database holds all of it. `stator_app` may only add its actor's own
view, for today, of a page they may view, and read only their own rows;
it may not change or delete one, nor touch the tally. The counts and the
names are `page_view_stats`, `page_readers` and `page_readers_unnamed`,
security definer functions like `home_updates` that keep to
`current_org_id()`, judge `current_actor_id()` with `perm_page_viewable`
and, for names, `perm_page_readers_listable` (edit without the archive
rule), once per call rather than per row. A trigger keeps anybody but the
person from changing their choice through `app_user`. On a page read 150
people a day for 90 days the counts take about 30 milliseconds and a window
of readers 5, from the indexes alone, which
`TestPageViewCountsReadAnIndexNotTheTable` holds.

The counts are a read tool for assistants, `get_page_views`; the names are
not, so who read what stays with the editors in the page rather than in
whatever an assistant passes on.

## 2026-10-02: A webhook sends as the administrator who saved it, read when it is sent

Outbound webhooks (#110) keep Armature's design: an endpoint is an address,
the topics it takes (`*` for every one), a secret shown in the answer that
made or rotated it and never again, and a log with one row per attempt.
The body is Armature's envelope (`id`, `topic`, `orgId`, `occurredAt`,
`payload`) and the signature its shape, `sha256=` and the hex HMAC of the
raw body, in `X-Stator-Signature-256` beside `X-Stator-Event` and
`X-Stator-Delivery`, so a receiver written for one product serves both. The
envelope's id is the outbox event's, the same on every retry and
redelivery, so a receiver can drop a repeat. An event is tried six times,
after 1, 5 and 30 minutes, 2 and 12 hours, as Armature tries; a test ping
or a redelivery is sent at once, even while the endpoint is off, and not
retried, since somebody is watching it. Only administrators keep webhooks,
as only Armature's tenant administrators do, and no operation is an MCP
tool.

Armature's payloads are its events as they happened, and every Armature
endpoint belongs to an administrator who may read the whole tenant. A wiki
page can be closed to its own space's administrators, and an administrator
can stop being one, so an endpoint here names an owner: the administrator
who saved it last, stamped by the database from the transaction's actor
and never written by the app. Each attempt reads the payload as the owner
through the ordinary row level security, so a page they may not view, or
one gone, comes back as no row and the attempt is withheld: nothing is
posted, nothing is retried, and the log says why. The outbox keeps only
ids, which the delivery copies; the words are read when they are sent,
never when the event was queued, so a page closed in between is not
posted and a redelivery reads afresh. Of a move, the old place's space and
parent are named only when the owner may view them. An owner who leaves the
organization leaves the endpoint without one, which then posts only pings
until somebody saves it again. Saving makes whoever saved it the owner
rather than keeping the first, because an endpoint whose owner left or
lost access would otherwise need a second operation to take it over.

The secret is sealed with `STATOR_SECRET_KEY`, bound to its organization
and endpoint, where Armature keeps it in clear. The app role may not select
the sealed column, the owner, or the failure counts, and may not write the
log at all; the worker, and a test or redelivery in the api, write the log
and open the secret as the admin role, acting for nobody, so sending a
test does not make the tester the owner. A restrictive policy holds the
app role to administrators of the organization for both tables.

Delivery follows the outbox's own pattern. The outbox handler queues one
delivery per enabled, subscribed endpoint, idempotently by event and
attempt, and runs before the notification fan-out, so a failed queue never
tells anybody twice. A sender in the worker leases due deliveries with
`SKIP LOCKED` before it posts, as the outbox worker leases events, so any
number of workers send each attempt once. Every request goes through
`netguard`, at every redirect, and an address that names the server's own
network outright is refused when it is saved; a delivery's error says what
happened in a sentence and never which host or port answered.

Armature has no automatic turning off. Here an endpoint that has failed at
least six attempts in a row, over at least a day, is turned off by the
worker, its waiting attempts cancelled, and the audit log records it with
nobody as the actor; turning it on clears the count. A day rather than a
count alone, so a receiver down for an afternoon under a burst of events
keeps its endpoint. Adding, changing, rotating and deleting an endpoint is
audited with its name and host only, since a receiver's path often carries
a token of its own. Attempts are kept 30 days, as Armature keeps them, and
pruned by the sender. Moving a published page to another parent or space
and moving one to the trash now write `page.moved` and `page.deleted`; a
reorder among siblings, a page the author has not published and the pages
that go with the one named write nothing.
## 2026-10-01: Stale pages are read from publishes and visits, by the administrators of their spaces

The stale content report (#99) lists published pages, out of the trash, that
nobody published again or opened for a period: 180 days unless the reader
names another, from 1 to 3650 through the API, 30 to 730 in the interface.
A page's last publish is `published_at` (#38), which only a new version
moves, so a rank change or a move does not freshen it. Its last view is the
latest `page_visit` of anybody, the row the recent pages already keep per
person; nothing new is tracked. Pages are ordered by the later of the two,
oldest first, and walked by keyset on that and the id, so a page published
or opened between two windows leaves the report rather than shifting it.

Who reads it is who looks after the space: its administrators for their
spaces, and the organization's administrators, who administer every space,
for all of them. A space administrator already passes every view list in
their space and decides its permissions and its trash, so the report tells
them nothing they could not find; a member who administers nothing is
refused with whom to ask, and the account menu offers the report only to
organization administrators, a space's settings to its administrators.
Each row is still judged by `perm_page_viewable`, which keeps out a page
below somebody else's unpublished page. Reading the report changes nothing
and leaves no audit entry, as reading the audit log does not.

Visits are each person's own under row level security, so the report is a
SQL function, `stale_pages`, run as the schema's owner like `home_updates`:
it keeps to `current_org_id()`, to the spaces `current_actor_id()`
administers, and answers when a page was last opened, never by whom. The
cheap conditions and the sort run first, over the spaces' published pages
with one probe of `page_visit_latest_idx` each for the last view, and the
view rule runs above the sort on as many rows as the window needs; on 4000
stale pages a window of 26 judges 26 and takes about 70 milliseconds, which
`TestStaleReportReadsAWindowNotTheWholeOrganization` holds.

Opening a page from the report to review it does not count as a view. The
link carries `from=stale` and the page then posts no visit, or working
through the report would empty it of every page the reviewer looked at
without deciding anything.

The report is also a read tool for assistants, `list_stale_pages`, as the
audit log is one for administrators: it runs as the token's person through
the same route, so an assistant helping a space administrator tidy up gets
the same rows, and a member's assistant the same refusal.

Stale pages are what archiving (#37) is for, so the report archives them in
bulk: the reader picks rows and the client calls the page's own
`PUT /pages/{id}/archive` for each, one after another, rather than through
an endpoint of the report's. Each call is then checked, audited and
refused exactly as from the page menu, a page already taken with one above
it is no change, and a refusal part way says how many went before it. Every
reader of the report administers the space, which is who archives, so each
row says only whether archiving can take it (`archivable`: not a home page,
which stands for its space, nor a page archived already) without asking the
rule again per row. Archived pages and pages of archived spaces leave the
report, as they leave the tree and search, and come back marked with
`archived=true`. Telling owners about their stale pages is left for later;
the verification reminder already tells them when a check runs out.

## 2026-10-01: MCP is the route table, dispatched in process as the caller

An assistant reaches Stator over the Model Context Protocol (#106) the way
it reaches Armature: a tool is a row of the operation table marked with a
name and a sentence, its input schema is that row reflected by the builder
that writes `api/openapi.json`, and a tool call is the HTTP call it stands
for, built in the api process and run through the router's whole chain as
the caller. Authentication, the read-only rule, the use check, the handlers
and the row level security apply to it without knowing it exists, so a tool
answers what the API answers that person and is refused with the same
sentence. A second set of handlers for tools would have been a second place
to get a permission wrong. The protocol is spoken with the standard library
over one stateless endpoint, `POST /api/v1/mcp`, which the integration
suite holds to the document like any other.

Callers bring a personal access token as a bearer, as in Armature; there is
no OAuth flow, since Armature has none and a token is already a person in
one organization that the tokens page makes and revokes. The issue asks
that a read-only token expose only read tools, which Armature does not do:
`tools/list` leaves out every writing tool for such a token, and a writing
tool called anyway is refused with the read-only sentence before its call is
made. The endpoint itself is exempt from the read-only middleware, since
it carries reads too; the calls it carries pass through that middleware
again.

Which operations are tools follows Armature's rule, reads and safe writes:
reading spaces, pages, versions, search, labels, comments, people,
notifications, a space's archive and, for administrators, the audit log;
and writing pages, labels and comments. Archiving and unarchiving are a
space administrator's, and are not tools. Nothing that deletes,
administers, changes who may do what, reaches other people's attention
(shares, reactions, watches, stars), vouches for a page (owners,
verification), or reorganizes the tree is a
tool; a person does those where they can see what they are doing. Armature
issues are left to Armature's own endpoint. Every operation of the table is
either a tool or declined with a reason in `mcp_test.go`, and a route added
without a decision fails the unit tests, so the choice is never made by
forgetting.

Pages are offered as Markdown besides their documents, through the
converter the page menu uses: Armature has no Markdown to follow, but a
model reads and writes Markdown far better than an editor's document. A
multipart operation can be a tool when its row names the file it sends:
the tool takes the text as `content` and sends it as that one file, so
`replace_page_markdown` and `import_markdown` are the Markdown operations
the page menu calls, not new ones. Nothing in the compose stack, the nginx
configuration or the chart changes, since the endpoint lives under `/api`.

## 2026-10-01: An archive is marks on the pages, frozen by the edit rule, and kept by space administrators

Archiving (#37) works as the trash does: the page archived and every page
below it still in the tree carry `archived_at` and `archive_id`, the page
archived, which makes them one item of the space's archive, and nothing
moves. A space carries `archived_at` of its own. A table of archived pages
would have copied every column a page has and lost the place, as a trash
table would have, and a mark on the top page alone would have made every
list walk up the tree to learn what it may show. With the marks on each
page, the tree, search, quick search and the home page's functions leave
archived pages out with one condition on the row they already read. Search
and the list of spaces include them when asked (`archived=true`, as
Armature lists archived projects), and each space's Archive tab lists its
items, which is how an archived page is found again.

Archived content is read only, and the database says so in the one place
every write already asks: `perm_page_holds`, behind edit and delete, and
`perm_page_commentable` are false for a page that is archived or in an
archived space. Every policy and guard that asks them (versions, drafts,
the page row, new pages below, files, labels, restrictions, owners and
verification, comments, reactions, passages, the trash) then refuses
without a rule of its own, and the service's `perm.ForPage` reads the same
state and answers 409 `archived` with a sentence that says to unarchive
first, rather than a 403 that would send the reader to ask for a permission
they already hold. Viewing, stars, watches and recent pages are not
changes of the page and stay. `page_place`, which administrators may use to
move pages they cannot edit, refuses to move an archived page out of its
item or anything under an archived page. The access inspector shows the
archive as a step of edit, delete and comment, naming the page archived.

Who archives is who administers the space, for pages and for the space,
as Armature's project administrators archive a project. Archiving takes
the right to change pages from everybody who has it, including people on
a page's edit list, which is more than delete does, and it is undone by
the same people; holding delete or edit alone would let one editor freeze
a colleague's work. The marks are written only by `page_archive` and
`page_unarchive`, security definer functions that check the actor
administers the space and may view the page; a trigger refuses the app
role any other change of them, and stamps who archived a space and when.
Only the top of an item is unarchived, and only while the page above it is
not archived, so a page that may change never hangs under one that may not;
a page restored from the trash under an archived page goes under the home
page for the same reason. The home page stands for the space, which is
archived as a whole.

Deleting a page above an archived one takes the archived pages to the trash
with their marks, as it takes pages the deleter cannot see: the trash is
the space's, and a restore brings them back archived. Archiving or
unarchiving does not change a page's `updated_at`, since no word of it
changed, and is written to the audit log, as `page.archived` and
`page.unarchived` with the number of pages, or `space.archived` and
`space.unarchived`.

## 2026-10-01: A share tells only who may already read, and refuses whole rather than in part

Sharing a page (#67) sends it with a note to people and groups. It never
grants anything: the page's restrictions and the space's permissions decide
who may read, and a share that let somebody in would be a second way to
change them that nobody administering the space could see. So the dialog
says who can already view the page, the picker marks whoever may not, and a
share naming such a person is refused with a sentence naming them, saying
what to do (remove them, or ask an administrator of the space or somebody
who may change the restrictions), and sending nothing to anybody. Sending to
the rest would have left the sharer to notice who was missing; a refusal is
noticed. The person refused is told nothing, so a share leaks nothing about a
page to somebody who may not read it. Asking for access on their behalf was
not built: the issue does not ask for it, and there is no request flow yet
for it to join.

A group is different: it names people the sharer usually cannot list, and
one member without access should not stop the rest hearing of the page. A
group tells those of its members who may view the page, the picker says how
many that is, and only a group none of whose members may view it is refused
like a person.

A share is a row, `page_share`, with the people it tells in
`page_share_recipient`, rather than a list inside the outbox event as
mentions are. The row is what the brake counts, what the worker reads whom
to tell from, and what the restrictive policies hold to the rules: the app
role may make a share only as the actor, of a published page out of the
trash they may view, and name only members other than themselves who may
view it; nobody reads anybody else's shares, and nobody changes or deletes
one. An outbox event about a share must name one the actor made in the same
transaction (its `created_at` is the transaction's `now()`), so a forged
event cannot send an old share again. The worker writes each row acting for
its recipient as for every kind, so access lost between the share and its
delivery tells nobody.

One person shares at most `share.MaxPerHour`, 30, pages an hour in an
organization. The service counts the last hour to say when to try again; a
trigger holds the same limit under an advisory lock per sharer, so two
shares at once cannot both slip under it, and `page_share_per_hour()` names
the number in SQL, which a test holds to the Go constant. Valkey would have
been the obvious place for a rate limit, but it is optional here, and the
rows are already where the shares are.

The note is at most 200 characters, `notify.MaxExcerptLength`, so the
notification carries it whole and the mail needs no field of its own. The
kind is `shared`, second in `notify.Kinds` after `mentioned`, since a share
too is addressed to one person; it has its own switches, on by default.

Each share is written to the audit log as `page.shared`, in its own
transaction, with the page, the people and groups named and how many were
told. The note is not recorded: it is a message to its readers, which the
administrators reading the log have no claim to, and it may hold anything.
A refused share records nothing, since nothing happened.

## 2026-10-01: A verification is a dated row beside the page, read as expired, and told by the worker

An owner (#68) and a verification are rows of their own, `page_owner` and
`page_verification`, rather than columns of `page`. `page_write_guard` and
the stamp of `published_at` read any change of a page row as an edit or a
move, so a verification there would have needed exceptions in both, and a
page's `updated_at` would have moved for something that changes no word.
Both go with their page when it is purged and stay with it in the trash; a
copy takes neither, since nobody checked the copy.

Who may change them is who may edit the page, on a published page out of the
trash: an unpublished page has one reader, so there is nobody to answer to.
Space and organization administrators pass edit lists already and need no
rule of their own. The database holds `stator_app` to the same through
`page_stewardable`, writes in the actor's own name only, an owner who may
view the page (`perm_page_viewable` of the owner), and a term of at most 730
days. When and at which version a page was verified are stamped by a trigger,
so a check cannot be backdated or claim a version nobody read, and only the
worker may write `lapse_noticed_at`.

An owner who later loses access is kept rather than removed: a restriction
that passes should not silently drop who answers for the page. The page says
so to its editors (`owner.canView`), and the reminder goes to whoever
verified the page instead. An owner who leaves the organization goes with
their membership. One owner, a person, as Armature's component lead is one
person: a group would answer for nothing in particular.

Whether a verification holds is read from its date (`expires_at > now()`)
wherever a page is read, so a badge is never stale between two looks of the
worker. The worker's part, `page.LapseWatch`, is what nothing in a request
would notice: every `STATOR_VERIFICATION_CHECK_INTERVAL` it finds lapses
nobody was told of across organizations as the admin role, as Armature's SLA
watch finds breaches, and in each organization marks the lapse and writes
`page.verification_lapsed` to the outbox in one transaction, so a second
worker or a second look finds nothing. The fan-out tells the owner with the
new kind `expired`, and the row is written acting for them, so the policy on
`notification` refuses it when they may not view the page. A renewed
verification clears the mark, and so does verifying again after a lapse.

Editing a verified page keeps the verification. It is a statement about a
time, with its own end, and dropping it on every typo fix would teach people
to ignore it; the details say which version was checked, and that the page
moved on since. Search and the home page's updates mark verified pages;
neither filters by it yet.

## 2026-10-01: The audit log is written with its act, read by administrators, and pruned only by the worker

The audit log (#107) keeps Armature's shape: one `audit_log` row per act,
naming the actor, the action, the kind and id of the target, a little data
and the caller's address, read back newest first with filters and exported
as CSV. Armature copies most of its entries off the event stream; Stator
writes each one with `audit.Write` inside the transaction of the act, as it
already did for members, tokens, spaces and permissions, so a change that
rolls back leaves no entry and one that commits always has one. Stator has
no stream to copy from, and an outbox entry would only say the same thing a
moment later, or not at all if the worker were down.

What is recorded: membership and single sign-on (let in, turned away,
removed, joined or moved by a group, the provider's settings, group
mappings), personal access tokens, spaces, the three kinds of permission,
deletions for good (purges, an emptied trash, a moderator's delete of a
comment), the organization's default theme, the Armature connection, and
exports. Page edits are not, for the reason labels are not: the page's own
history holds them. An export changes nothing, so its entry has a
transaction of its own, written before the file is sent; an export the log
cannot record is refused, so none leaves unrecorded. Exporting the log is
itself an entry.

Only ids, names and whether a secret changed go into an entry. `audit.Scrub`
replaces any text under a key named like a credential, other than set, kept
or removed, and any text that starts as a Stator or Armature token does, with
`[redacted]`, so a slip in a caller leaves the record clean rather than
failing the act.

The database holds the rest. A restrictive policy lets `stator_app` read the
log only for an actor who is an owner or admin, the same test `requireAdmin`
makes, and another lets it write only entries naming the transaction's own
actor or nobody. Neither runtime role may update or delete an entry.
Retention is `STATOR_RETAIN_AUDIT` (a year by default, as Armature keeps
its log; 0 keeps everything; less than a day is refused), and the worker
enforces it once a day as `stator_admin` through `audit_log_prune`, a
security definer function only that role may call, which works inside one
organization and refuses a cutoff younger than a day whatever it is handed.

The list walks `(created_at, id)` by keyset (#38), so new entries arriving
between two pages neither repeat nor skip one; each filter has an index that
ends in that order. Days in a filter are the reader's own: the client sends
the instants their midnight falls on, and the API also takes a bare date as
that day in UTC.

## 2026-10-01: A star is a row of one's own, and the home lists walk an index a window at a time

A star (#38) is a row naming its person and either a page or a space, as
Armature keeps its starred filters: `PUT` and `DELETE` on the thing's own
`/star`, no body, and `starred` on the page and the space. Like a watch it is
only ever one's own and only on what one may view; restrictive policies hold
`stator_app` to both, and the app role may not update a star at all, only put
it on or take it off. A star on a page that became restricted, or went to the
trash, is kept and read by nobody until the page opens again, so a passing
restriction does not lose it. A home page's header star stands for its space.

The home page has four lists: stars, the pages one viewed last (the visits
search already kept), the pages one edited (a version, a draft, or a page
made and never published), and the updates others published, everywhere one
may read or, with `scope=watched`, what one's page, subtree and space watches
cover. Updates are one row per page, its latest version, because a page
saved twenty times in a morning is one thing to read, and they leave out the
reader's own, which the edited list already holds.

The lists that grow without bound are read by keyset, `(time, id)` from an
opaque cursor, newest first, so a window neither skips nor repeats a row when
somebody publishes between two reads; offsets would. Ordering by
`page.updated_at` would have floated a page up for every move, rank change
and trash mark, so a page now carries `published_at`, stamped by a trigger
when its version changes and by nothing else, which also keeps the app role
from moving a page up the feed. `page_published_idx` serves the walk.

Updates and edits are SQL functions, `home_updates` and `home_edited`,
rather than queries the service runs as `stator_app`. Row level security
puts its policy ahead of every condition of a query, so `perm_page_viewable`
ran on each row the index scan met, hidden spaces included: on an
organization of 6000 pages, 2000 of them in a space the reader may not view,
a window of 21 took 3.2 seconds. The functions run as the schema's owner,
filter the organization themselves, and decide the order: the spaces the
reader may view and what they watch first, the view rule only then, in a
`CASE`, since the planner had also turned a plain `IN` into a join after the
rule. The same window takes 60 milliseconds, the rule runs once per row
returned, and `TestUpdatesReadAWindowNotTheWholeOrganization` holds both:
it counts the rule's calls and reads the plan with `auto_explain`.

## 2026-10-01: The access inspector asks the database, and only administrators ask

Explaining why somebody may or may not open a page (#81) could have been a
second copy of the rules in Go, laid out as steps. A copy drifts: the day a
policy changes and the copy does not, the inspector tells an administrator
the wrong reason with full confidence. So every verdict, and whether each
step is met, is what the SQL functions behind the policies answer for that
person (`perm_page_viewable`, `perm_page_editable`, `perm_page_deletable`,
`perm_page_commentable`, `perm_space_holds`, `perm_page_lists`), and the
grants and list entries named beside a step are matched with the same
`perm_subject_matches`. Go only orders them. The integration suite acts as
each person straight through SQL and holds the inspector to what the
database lets them do.

Only administrators of the page's space, or of the organization, may
inspect, themselves included. The answer lays out the space's permission
table and the global grants, which are theirs to see and nobody else's; a
member who wonders why they cannot edit is told by the refusal itself whom
to ask. The global grants are hidden from space administrators by their
policy, so `perm_global_grant_sources` (migration 00200) names the use grants
that reach a person to anybody who administers a space, the organization,
or is that person.

The home page reports the right to move it to the trash as the rules give
it, although a check keeps every home page out of the trash; the page never
offers it.

## 2026-10-01: Markdown is converted on the server, and what it cannot say is a marked element

Import and export run in the API rather than in the browser. A script with a
personal access token then gets the same pages as the page menu, every page
an import makes passes the one allowlist the editor's saves pass, an export
reads the files and the pages below with the caller's own permissions in one
place, and the archive is written and read where the files are. The editor's
Markdown paste stays in the browser: it is typing, not moving pages.

Markdown is read by goldmark, MIT licensed and kept up, with the GitHub
extensions for tables, task lists and strikethrough, and written by our own
serializer, since a document has to come back as the same document and no
library writes this allowlist. Armature has no Markdown handling to follow.

What Markdown has a syntax for uses it, so an export reads well in a
repository: panels as GitHub's alert quotes, expand blocks as `details`, task
lists as check boxes. What it has none for (a mention, a status, a date, an
Armature issue, a table of contents, child pages) is a `span` or a `div`
marked `data-stator`, whose text is what the reader would see. Any renderer
shows the words, and an import reads the exact form back into its node,
checked by the allowlist like anything else. An HTML comment would have
hidden the words, and a fenced block of JSON would have read as noise; both
would have needed the attachment ids rewritten as well.

Raw HTML never reaches a page. The tokenizer reads only the forms above and
`img` and `br`; an unknown block is shown as its source in a code block, and
an unknown inline tag is dropped with its words kept, so a README full of
badges and `kbd` still reads. Parsing is bounded before it starts: by file
size, by how deep one line nests, and by how many brackets the long lines
hold, the inputs that cost the parser time with the square of their size.

An import makes every page unpublished, attaches its files, and only then
publishes them, since the links between pages need the pages' ids and the
pictures need the files' ids. A failure part way trashes what was made, and
nobody else has seen any of it. `docs/markdown.md` lists how each node is
written and what does not come back as it left.

## 2026-10-01: The outbox worker leases events instead of holding their locks

A push gate (#217) hung for ten minutes in a test's cleanup, `DELETE FROM
org`. The worker claimed a batch with `FOR UPDATE SKIP LOCKED` and kept the
transaction open while the handlers ran. A handler writes on connections of
its own, and a write that names the organization (a notification, a link)
waits for a delete of that organization to end; the delete cascades to the
claimed events and waits for the worker's transaction, which waits for the
handler. Postgres sees no deadlock, since one side of the cycle is the
worker's Go code, so both wait for ever. Deleting an organization while its
events are being handled would hang the same way in production.

The worker now claims a batch in a transaction of its own that sets each
event's `available_at` a lease ahead (`events.ClaimLease`, five minutes) and
commits, then handles each event and marks it in another short transaction.
Other workers skip a leased event as they skipped a locked one; a worker that
dies holding a lease delays its events by the lease, not for ever, and one
that stops hands back what it had not reached. Delivery stays at least once.

The integration harness bounds its cleanup statements (30 s) and logs who
blocks whom from `pg_stat_activity` and `pg_locks` once one runs for 10 s, or
a test for two minutes, so a lock wait fails one test with its cause instead
of the whole package with a goroutine dump.

## 2026-10-01: The interface speaks German and English; the server stays English

Every string the web client shows lives in `web/src/i18n`, and #113 adds a
German catalogue held to the English one's type, so a key added in English and
not in German fails `tsc`. The two are plain objects of strings and small
functions, as before; a library would bring plural rules and message syntax
two languages do not need, and a second way to write a string.

The choice is the person's, stored on their account in the `locale` column
that came with the first migration, as Armature keeps its language there and
changes it with `PATCH /auth/me`. Armature's column defaults to English;
Stator's is null until the person chooses, because the issue asks for the
browser to decide until then, and every row the old default wrote was made
null, since nothing had ever set one. A check constraint holds it to `en` and
`de`. The browser remembers the choice too, so a reload paints in it before
`/auth/me` answers; a browser that never saw it takes it from there.

`t` is a live binding that switches with the language, and the router is
drawn again under a key of the language, because strings are read while
rendering: a module keeps no piece of `t` at its top level. Dates and numbers
go through `web/src/lib/format.ts`, which looks the locale up on each call and
keeps the browser's own variant of the language shown, so en-GB keeps its day
before the month.

German addresses the reader as Sie, the convention for software used at work;
Armature has no German interface yet whose choice could be followed. Names of
Armature's screens stay as Armature shows them. Quotes stay ASCII, as the
house style asks, and the strings avoid needing them. Emoji are found by their
English names, and the German search says so.

What the server writes stays English for now: notification mails and digests,
and the sentences of error answers the client shows as they come. Translating
them means choosing the language per recipient in the worker and per request
in every handler, which is its own change; the profile says so in German.

## 2026-10-01: The push gate runs before a background push starts, not inside it

Two agent pushes (#205, #212) looked as if they had skipped `./run-tests.sh`:
the background push finished two seconds after it was reported as started.
Their transcripts show it did not skip. A `PreToolUse` hook runs before the
tool, and for a Bash call with `run_in_background` Claude Code waits for the
hook before it starts the background task, so the whole gate (15m51s for
#205, 9m37s for #212) passed while the call itself was pending, and the
push that ran afterwards had nothing left to wait for. A push from a worktree
here behaved the same, with the gate running in the worktree. This is how the
harness orders hooks and background tasks, not something the guard can
change, so the guard is left as it is and the time to look at is the delay
before "Command running in background", or the `gate_secs` the guard logs.

Every hook invocation now appends a line to `branch-guard.log` in the common
git dir, so the next doubt is settled by reading it rather than by replaying.
Worktree agents run with `CLAUDE_PROJECT_DIR` set to the main checkout, so
the hook that guards them is the main checkout's working copy, not their
own: a change to the guard protects agents only once the main checkout has
it, and the log's `hook=` field says which copy ran.

## 2026-10-01: A reaction is a row per person and emoji, given where one may comment

A reaction (#66) is one row naming its page, the comment when it is on one,
the person and the emoji, unique over the four. A comment's reaction names
the comment's page too, and a foreign key to the comment's id and page keeps
the two together, so the page's view rule reads every reaction without a
join: the same `perm_page_viewable` decides who sees a reaction as who sees
the page. Nothing notifies: a reaction is meant to be lighter than a reply,
and the issue asks only for the reactions and who gave them.

Putting one on takes the right to comment, `perm_page_commentable`, on a
published page out of the trash. A reaction is feedback in the discussion,
so a space that keeps somebody from commenting keeps them from reacting,
and an administrator has one switch for both. Taking one's own off needs
only view, as deleting one's own comment does. The database holds the app
role to the same with restrictive policies, and to the actor's own name; it
may not update a reaction at all. A deleted comment is a placeholder and
takes none: a trigger refuses a new one with a share lock on the comment, so
it and the delete wait for each other, and the delete takes the existing
ones with it, as it takes the notifications.

The API takes any one emoji, checked in Go by its code points (`reaction.Clean`)
and in the database by length and by having no letters or spaces, so the web
client's picker can grow without a change on the server; a person puts at
most 20 different emoji on one thing. Each reaction answers its count,
whether the caller gave it, and the first ten people by name, earliest
first, which is what the tooltip says: the caller first as "You", then the
names, then how many more. The names travel with the page and the comment
lists, so hovering asks nothing of the server.

The picker offers eight common emoji in a menu and, under "More emoji", a
search of the same bundled list the editor's colon uses (#43), found by the
same ranking. The list and its matching moved to `web/src/lib/emoji.ts`, away
from the editor's extension, so searching for a reaction loads the list but
never the editor. Every emoji of that list passes `reaction.Clean`.

In the web client each emoji is a toggle button with `aria-pressed`, and who
reacted is both its tooltip and its description, so a screen reader hears it
on focus. Somebody who may not react can still focus the buttons, which are
`aria-disabled` rather than disabled, to learn who reacted.

## 2026-10-01: A status is words on a theme tint, a date is a day, an emoji is text

A `status` node stores its words and one of five colours by theme role
(`neutral`, `accent`, `success`, `warning`, `danger`), never a colour value,
so a custom theme recolours it as it does panels and cell backgrounds. It is
drawn as Armature draws an issue's status: the words in `ink`, upper case, on
the role's subtle tint, which holds AA contrast in every built-in theme
without a text colour per role. Since the words are the author's, they carry
the meaning and the colour only groups.

A `date` node stores a day as `YYYY-MM-DD`, not an instant, and the reader's
browser formats it in their locale, read in UTC, so a day is the same day for
everybody wherever they are. The server refuses a day that does not exist.
The picker is the browser's own date field in a dialog, which is keyboard
operable and speaks the reader's format without a calendar of our own.

Both are found by search: the database's plain text reads a status by its
words and a date by its `YYYY-MM-DD`, as `document.PlainText` does. Neither
may go in a comment, which holds text and its structure only.

An emoji is a character of the text, not a node: it reads, copies, searches
and diffs like any other, and needs nothing on the server. The names a colon
finds it by come from gemoji, bundled with the client under the MIT License
and loaded with the first colon, so no emoji is ever fetched from elsewhere.

## 2026-10-02: Columns store a share of their row and stack on a narrow screen

A column layout is a `columns` node holding two or three `column` nodes,
each with any blocks a panel takes. The bound on how many is the
allowlist's: a node spec now names the fewest and most children it takes,
which the server and the web editor's check read alike, because one column
is just the page and a fourth is too narrow to read beside the text. A
column inside a column is allowed, since a table cell or a panel inside one
is, but the editor does not make one: the room left would be too little.

Each column stores its `width` as a share of the row, a whole percent from
10 to 80. The shares are read as proportions, so a body written elsewhere
whose shares do not add up still lays out, and a column with none stored
takes an even share. Storing shares rather than pixels keeps a layout the
same on every screen. The editor offers named layouts, even or with one
column wider, rather than dragging a border: they work the same from the
keyboard and on a touch screen, and they give readers the same few shapes
across pages. A layout with fewer columns folds the blocks of the ones it
drops into the last column it keeps, so changing the layout loses nothing.

Under 48rem, the shell's own breakpoint, the columns stack in their
reading order: side by side, each would be a few words wide. Markdown has
no columns, so an export writes the blocks one column after another, as a
phone shows them, and an import of that file brings them back as plain blocks.

Search needs no change: `document.PlainText` and the database's
`page_plain_blocks` already read the blocks inside any node they do not
name, column by column.

## 2026-10-01: An expand block stores its title, never whether it is open

An expand block is one node, `expand`, with a `title` attribute of up to 200
characters and the same blocks inside it as a panel takes. The title is an
attribute rather than a child node of its own because it is one line of
plain words on a button: a node would have let marks, mentions and line
breaks into a toggle, and would have needed rules to keep it first and
alone. An empty title is allowed and reads as "Details", so a draft saved
before the author names it is still a page the API takes.

Whether a block is open is not stored. It is each reader's own and lasts for
their visit: a stored state would make one author's click everybody's
default, and would make opening a section a change to publish. Readers find
every block closed; the editor shows every block open, since an author
edits what is inside. A comparison of versions and a template's preview
show them open too, since they exist to show everything, and so does a
print, which is how a PDF is made: paper cannot be clicked open. The read
view keeps a closed block's content in the page, hidden by its CSS, so the
print stylesheet can show it without the view knowing it is printing.

The toggle is a `button` with `aria-expanded` and `aria-controls` rather
than `details` and `summary`, so the state is told the same way in every
browser and to every test, and the reader's view can open a block when a
link leads to a heading inside it: following a table of contents entry or
arriving with the heading's address opens every closed block around it
before scrolling there.

Search reads the title as a line of the page's words, then the blocks
inside, in `document.PlainText` and in the database's `page_plain_blocks`
alike (migration 00176), so a page is found by a word folded away.

## 2026-10-01: A danger button writes its label in `on-danger`

The danger red does two jobs: it colours error text and destructive menu
items on the surfaces, and it fills the danger button. In the dark palettes
it has to be light to read as text on dark surfaces, and white on a light
red reaches only about 3:1. So the label gets a token of its own,
`on-danger`, as the accent and the primary action already have: white in
the light palettes, a near black of the same hue in the dark ones. A theme
that leaves it out gets the built-in value for its mode, which suits the
deep reds of a light palette and the light reds of a dark one. Darkening
the dark red instead would have failed the error text it also draws.

## 2026-10-01: A followed Armature theme is kept as the person's hidden copy

Following stores the theme Armature shows a person as a theme of theirs,
marked as the copy of Armature's by its id and `updatedAt`, rather than
pointing the browser at Armature. Its files are then served by Stator, the
browser never needs a session in Armature, the copy is checked with the
same code as an imported theme, and it is downloaded again only when
Armature answers another id or a later time. The copy is left out of the
themes list and cannot be edited, shared, exported or chosen, and the
database says so too: a check keeps it private and a trigger keeps it out
of `user_theme`. Following is a mark on the person's `user_theme` row that
names no theme, so choosing a theme ends it by the same constraint.

Which theme Armature shows is cached per token row for five minutes, as the
contract says, because Armature announces no theme change by webhook. The
theme settings ask Armature at once instead, so a person who just changed
their theme there sees it by opening the settings. One 2 second budget
covers both calls to Armature on a page load, the active theme and the
download, and an answer that misses it falls back to what the person would
see without following; the copy is never written halfway, since saving it
is not bound by that budget. A theme that fails the checks is remembered
with its reason in the cache entry, so a broken theme is not downloaded on
every page, and following one is refused with that reason.

## 2026-10-01: The webhook receiver tells nobody which organizations exist

`POST /armature/webhook/{orgSlug}` needs no sign-in, so anybody can post to
it with any slug. An organization that does not exist, one that has not
connected Armature or saved no secret, a wrong signature and a body naming
another Armature organization are all one answer, 401 `bad_signature`, with
the same sentence; when there is no secret the signature is still worked
out against a stand-in, so the time taken does not tell them apart either.
The contract first answered an organization without a secret 404, which
would have listed the organizations that use Armature to whoever asked.

The secret is read as the admin role by the organization's slug, as the
sign-in paths are, because no tenant is known until the signature vouches
for the body. Replays are refused by the event id Armature keeps across
retries, remembered in Valkey for 24 hours with `SET NX`, rather than by
`occurredAt`, which a genuine redelivery can carry hours old. A refused
delivery leaves no mark, so a forged copy of an event cannot make the
genuine one look like a replay. Topics the receiver does not act on are
204 and leave no mark either, so subscribing to more topics than needed
costs nothing but the request.

## 2026-10-01: Page links in Armature follow every change, as whoever made it

A page's remote links in Armature are synced by the worker from an
`armature.links` event `{pageId, actorId}`, written in the transaction of any
change that can change them: every publish (a copy's too, since a copy is a
page of its own), a move to another space, a change of restrictions or of the
space's permissions, trashing, restoring, purging, emptying the trash and
deleting the space. The event carries no keys; the worker reads what the page
names when it runs, so a late event is never stale and a second one does
nothing. It runs as the event's actor with their own token, so Armature
records who linked the page, and a page can only appear on issues its author
may edit. A service token would have linked every issue any member names and
hidden who did it.

Changes that reach pages below the one named, which the actor may not all
see, go through `armature_links_emit` in the database, which writes the
events itself rather than answering which pages they are. Only pages that
name an issue or carry links get one, and none while the organization has no
connection.

`armature_remote_link` records, per page and key, the link's id in Armature,
the url and title sent, and the state. It has no foreign key to the page, so
the sync after a purge still finds what to take off. A link is put again
only when it is not synced as wanted, and Armature retitles the link with the
same url, so sending twice is harmless. An Armature that does not answer, a
429 or a 5xx fails the event, which the outbox tries again; what was done
before is kept. A refusal another try would meet again (no token, a rejected
or read-only token, 403, 404) marks the key failed with a sentence and lets
the event finish, so one bad key never holds the others; the next change to
the page, by anybody, tries it again.

A page is titled "A restricted page in Stator" unless nothing above it
carries a view list and its space lets everyone view it. A space opened to a
group that happens to hold every member counts as closed, which errs towards
telling Armature less.

The worker now routes events by topic (`events.Mux`), so a link sync that
fails is retried without telling anybody about the publish twice. The
integration suite seals tokens with the stack's own `STATOR_SECRET_KEY` and
syncs with the stack's `STATOR_APP_URL`, because the stack's worker drains the
same outbox beside the suite's and has to reach the same result.

## 2026-10-01: Issues from a selection are filed one by one, from a toolbar row

Armature has no batch create, so `POST /armature/issues` files the items in
order with Armature's own `POST /issues`, as the caller with their token, and
stops at the first refusal: a refusal of the project, the type or the rights
would meet every later item the same way, and stopping leaves the author one
place to look. The answer is 201 with the issues made and the refused item,
so the page gets the chips of what exists in Armature; a refusal of the first
item is the answer itself, since nothing was made. Nothing is retried,
because a create that timed out may have reached Armature, and a second one
would file the issue twice.

The action sits in a "Selection" row of the editor's toolbar, beside the
table, code and heading rows, rather than in a menu floating over the
selection. The editor has no floating menu yet, the row is reached with the
same keys as the other tools, and it does not cover the text being read.

All chips go in with one transaction that closes the history group, so one
undo takes back the whole edit and not a chip at a time. Selected text is
replaced by its chip, which says the same thing with the issue's live
status; list items and table rows keep their text, which often says more
than the summary, and the chip follows it. A row is named by its first cell
with text and its chip follows that cell's text, so a row whose first cell is
empty still gets a chip where the words are.

## 2026-10-01: An issue list stores its query, and each reader asks for their own rows

`armatureIssueList` keeps the query, the columns and the most rows, never
the rows: what a query finds depends on who asks, since Armature answers
`currentUser()` and the projects the reader may see, and rows in the body
would be readable by everybody who reads the page and would go stale. Each
view asks `GET /armature/search` as the reader, cached per token row for a
minute, so twenty readers of one page cost Armature twenty searches at most
once a minute each.

A query Armature cannot read is the author's to fix, so the search answers
422 `bad_query` with Armature's sentence and the position rather than a
status, and the block shows where it went wrong. The settings dialog checks
the query with the same route and `limit=1` as it is typed, and refuses to
save a query Armature refused, so a typo is seen before it is published.

Rows arrive a page at a time and "Show more" asks for the next page up to
the block's limit, rather than all of them at once, so a list of a hundred
rows does not hold the page while Armature answers. Sorting by a column
orders the rows already fetched; ordering on the server is the query's own
`ORDER BY`, which the author writes.

The allowlist gains the `strings` kind for the columns: a list of distinct
values from an enum, with `minLength` and `maxLength`. The web client's
test reads the same rule from `api/document-allowlist.json`.

## 2026-10-01: An issue block is a card drawn from the chips' lookup, and its picker checks the key

`armatureIssueBlock` stores the key and nothing else, for the reasons the chip
does. Its card reads the same `ArmatureIssuesProvider` as the chips, whose
lookup already answers every field the card shows, so a page with blocks and
chips asks Armature once, and the card needs no request of its own. Without a
token, or for an issue the viewer may not see, the block shows the chip's
words inside its frame rather than an empty card, so a reader learns the same
thing from both.

The picker inserts a key only once `GET /armature/issues/{key}` finds the
issue for the author, as the contract asks, and stores the key the issue has
now: a block inserted today should not name an issue by a key it left. It
reads keys and issue addresses alike, since people copy either. Searching by
words waits for the search route of #30; a key is what authors have at hand
when they embed an issue.

A comparison of versions describes the block in words, as it does the
generated blocks, because what the card shows is the issue now, not the
version's.

## 2026-10-01: An issue chip is its key, and each view asks Armature once

An `armatureIssue` node holds the key and nothing else. A summary in the body
would be readable by everybody who may read the page, whether or not
Armature lets them see the issue, and it would go stale. Each view collects
the keys it names and asks `GET /armature/issues` once per 50, sorted so the
same keys make the same request; the server answers from the person's cache
and asks Armature for the rest with one NQL search and then, eight at a time,
`GET /issues/{key}` for each key the search missed, which is how an issue
moved to another project is found by its old key. A chip of a moved issue
shows the key it has now and keeps the one the author wrote. Keys asked twice
are answered once. A lookup that cannot ask Armature answers its status and
no issues, not the part the cache holds, so a page never shows some chips
as Armature answered and others as if it had.

Typing a key makes a chip only in a project the author sees, which needs
`GET /armature/projects` from #31; #28 builds it. The `meta` entry holds the
projects and the issue types together, fetched together, so the create
dialog of #31 finds both in one entry.

The hover card explains and holds nothing to press, so it is a tooltip that
focus opens as well as the pointer. The way to connect an account is a link
inside the chip itself rather than a button in the card. In the editor a
chip is not a link: a click there selects it, as it selects any other atom.
A comparison of versions draws chips live, as the reader sees them now.

## 2026-10-01: The database forgets Armature tokens when the address moves, and each member reaches only their own

An administrator who could point the connection at a host of their choosing
while the members' tokens stayed would collect those tokens on the next page
view. So a new base URL or Armature organization deletes every stored token
in the same statement: `armature_connection_moved`, a trigger, does it, so
raw SQL as `stator_app` cannot move the address and keep them, and removing
the connection takes them by a foreign key. The same trigger forgets the
Armature organization id learned from the first token, which only the
service writes, as the admin role: a member could otherwise pin it with raw
SQL and have every genuine webhook refused.

`stator_app` reads and writes only the actor's own `armature_token` row, and
only administrators the connection. What a member needs of the connection,
its address and slug, and what an administrator needs of the tokens, how
many there are, come from two SECURITY DEFINER functions, so neither reads
the other's rows. A token is never rewritten in place and a row id is never
chosen: storing a token makes a new row with a fresh id, and the cache keys
every answer by that id (#28 onwards), so a new token, or a new Armature
identity behind it, never reads what was cached for the old one.

Saving the connection asks Armature for its OpenAPI document only when the
address changes. Rotating the webhook secret or naming another organization
then works while Armature is down, and the tokens are still forgotten by the
database either way. A check that finds Armature unreachable records it and
keeps the token, since an outage says nothing about the token; only a 401
marks it rejected.

## 2026-10-01: One service builds the backend image, the others run it

The stub is a Go binary in the backend image like every other service. The
classic builder tags that one image from each service's build at once, and
with a fifth service building it the tagging raced often enough to fail
`make stack-up` with "already exists". The stub named the image with
`pull_policy: never` instead of building it, but the four that still built
it kept racing (#247): of three stacks built side by side, two failed in
the first round. Since then migrate alone has the `build` section, and
every other Go service names the image with `pull_policy: never`. Compose
builds before it creates containers, so the image is there when any of
them starts.

## 2026-09-30: Deleting somebody else's comment is moderation, and takes space administer

Since #176, a space's `delete` let its holder delete anybody's comment, and
a new space grants `delete` to everyone (#159), so by default every member
could remove every other member's words. The product owner decided (#180)
that removing another person's comment is moderation, not housekeeping of
pages: it takes the space's `administer`, which space and organization
admins hold as #19 defines. The space's `delete` keeps its meaning for pages,
trash and restore, and no longer reaches comments. Authors still delete
their own while they may view the page, and a moderator's delete is still
audited as `comment.deleted`.

The rule is `perm_comment_deletable`, which migration 00155 redefines, so the
service, the `can.delete` flag the web reads and `comment_write_guard` for
raw SQL as `stator_app` all ask the same question. Blanking a comment without
deleting it is no way around it: the body is the author's alone to change,
and the table's check keeps a null body tied to a delete.

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

## 2026-09-30: A mention is read from the document, and the database holds it to members

Mentions keep no table of their own. On every publish the service compares
the mentions of the new version with the one before, and on a comment with
the words it replaces, and the outbox event carries the people named for the
first time. What a page or comment says is the only record, so nothing can
drift from it, and a republish or a restore finds nobody new.

The allowlist holds a mention's id to a uuid, and the service keeps only
members who may view the page as the change commits; the worker checks again
when it writes each row acting for its recipient. A mention of somebody who
left, or never belonged, stays in the text and tells nobody, as the contract
says, instead of refusing a save over a name the author cannot fix. What the
database adds is that an outbox event may mention members alone, so a
forged event cannot reach a stranger.

A comment edit tells only the people it adds, and the worker leaves out
anybody who already has a notification about that comment, of any kind. The
rows are the memory: a name removed and put back does not tell anybody
twice, and no table of who was told has to follow deletes and purges.

The picker's `canView` comes from `perm_page_viewable_published`, the view
rule without the unpublished rule, as a SQL function beside the others, so
the author of a new page sees whom a mention will reach once it is
published. The editor looks people up after `MENTION_SEARCH_DEBOUNCE_MS` of
quiet and narrows the last answer at once while the next is on its way; a
lookup the list outlived is aborted and its answer dropped. The development
editor keeps its fixed people, which the touch and keyboard specs rely on.

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
may view the page, or anybody's who holds the space's `administer` (see
the entry on moderation above). A policy
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

The browser suite lives with what this leaves out (#291). A read by anybody
but the writer, bob after alice, an anonymous reader, a token, a session
signed in afresh, or any read after a write by the Armature stub or by a job,
may reach a replica that has not replayed the write. Such a read retries,
through `e2e/fixtures/replica.ts`, until it sees what only the newest write
shows, and an absence only once the page has shown it loaded. One retry does
not cover the reads after it, since a read can reach the primary and the next
one a replica still behind. Rows a spec writes straight in the database are
waited for once, as the integration suite's `settle` does: `withDatabase`
returns when every replica has replayed them. Timeouts stay as they are, and
the app's consistency does not bend to the tests: a person who misses their
own write is a bug.

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
