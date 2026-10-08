# Importing another wiki's space

Whoever may create spaces makes a new space of the space export another wiki
wrote, from **Import space** on the spaces overview, the same way a Stator
archive is imported (`docs/decisions.md`, 2026-10-07 and 2026-10-08). The
api tells the two apart by what the uploaded zip holds; the worker reads the
export (`backend/internal/wikiread`) into the archive's shapes and writes it
with the archive's importer (`backend/internal/spaceio`).

## Operations

| Operation | Who | Answers |
|---|---|---|
| `POST /space-imports?key=KEY&name=NAME` | whoever may create spaces | 202 with the job; 422 when the zip is neither an archive nor an export, when an export comes without a key or the key is taken; 413 past `STATOR_SPACE_IMPORT_LIMIT` |
| `GET /space-imports/{importID}` | its requester, an administrator of the organization | the job: `source` is `archive`, `html` or `xml`; once done, the report with `lost`, page by page |

The key is required for an export: the other wiki's key is its own, and
the importer chooses the one the space has here. The name is the export's
own when none is given, else the key. Queuing is held by the database as a
space archive's is: by whoever may create spaces, for themselves, with a
key for an export.

## What is read

Both formats come as a zip, its files at the top or in one folder.

**An HTML space export** is a folder of one HTML file per page:

- `index.html`, whose title is the space's name and whose nested lists of
  links to the page files are the page tree. The list linking the most
  pages is taken as the tree.
- A page file each, named after the page and its id (`Setup_12345.html`).
  The title is the element with the id `title-text` (or the class
  `title-text` or `page-title`), else the first level 1 heading outside the
  content, else the document's title without the space's name before it.
  The content is the element with the id `main-content` (or the class
  `wiki-content`, `page-content` or `main-content`), else `main`, `article`
  or the body.
- Breadcrumbs (the id `breadcrumbs` or the class `breadcrumbs`) place a page
  the index's tree does not list: under the last page they name.
- Who wrote it: in the element with the class `page-metadata`, elements of
  the class `author` or `editor`, the first the creator and the last who
  changed it last; a `mailto:` link inside gives their address. Dates are
  `time` elements' `datetime`, else dates written in the words.
- Labels: the links or list items in the element with the class `labels`
  (or the id `labels`).
- Comments: elements of the class `comment` in the element with the id
  `comments-section` (or the class `comments`), each with its `author`, a
  `time` or `date`, and its `comment-body`; a comment inside another is a
  reply in its thread.
- Files: `attachments/{page id}/` beside the pages, the page id being the
  digits at the end of the page file's name, or the name itself. A link to a
  file whose words are a file name gives the file that name. A picture
  elsewhere in the zip that a page shows becomes a file of that page.

When the index's tree has one root, it is the home page. Otherwise the index
becomes the home page, its words kept, listing the pages below it, and every
root hangs from it.

**An XML space export** is a zip of `entities.xml` and the files beside it:

- `entities.xml` holds `object` elements, each with a class, an `id`, and
  `property` elements by name, a value or a reference to another object by
  its id, and `collection` elements of ids. Read are the classes `Space`
  (key, name, home page, description), `Page` and `BlogPost` (title, space,
  parent, position, version, status, creator, last modifier, dates, version
  comment; an older version names the current one in `originalVersion`),
  `BodyContent` (the body of the object `content` names), `Comment`
  (`containerContent`, `parent`), `Attachment` (title, `containerContent`,
  version, `originalVersion`, `contentType`), `ContentProperty` (a file's
  `MEDIA_TYPE`), `Label` and `Labelling`, and every class with User in its
  name (name, full name, email). Drafts, deleted content and personal labels
  are passed over, and every other object as it is read, so a large export
  is never held whole.
- A body is XHTML with elements of the other wiki's own in undeclared
  namespaces: links and pictures name pages by title, files by name and
  people by key; macros carry a name, parameters and a rich or plain body.
- Files are at `attachments/{page id}/{file id}/{version}`.

## Blocks

| In the export | Here |
|---|---|
| `p`, `br`, `hr` | paragraph, hard break, horizontal rule |
| `h1` to `h6` | heading 1 to 3; deeper headings are level 3 |
| `ul`, `ol` (with `start` and `type`) | bullet list, ordered list |
| a list whose class says task, or whose items open with a check box; task lists of the XML export | task list, ticked as the item's class or box says |
| `pre`, and code macros with their language | code block, its language from `language-*`, `brush:` or the macro's parameter |
| `blockquote` | quote |
| `table` with `th`, `td`, `colspan`, `rowspan` | table with header cells and merged cells |
| an element whose class names a callout (info, note, tip, success, warning, error, danger, caution), and the info, note, tip, warning and panel macros with their title | panel of that kind |
| `details`, an element whose class says expand with one that says content, and the expand macro | expand with its title |
| an element whose class is `toc`, and the toc macro | table of contents |
| the children macro | child pages |
| the attachments macro | the files list |
| the status macro | status in its colour |
| a page layout's sections, and section macros, of two or three columns | columns |
| `img` of a file of the export, and pictures of the XML export by file name | picture of its page, its description and width kept |
| `time datetime` | date |
| `strong`, `em`, `s`, `code` and their kin, and bold, italic and struck styles | bold, italic, strike, code |
| a link to a page of the export | a link to that page in the new space |
| a link to a file of the export | the file's chip when its words are the name, else a link to the file |
| a link to the web or an address | the link as it was |
| a mention of a person | their name after an @, as words |
| emoticons | the emoji they stand for, or their words |

A comment holds what a comment may: a block a comment cannot hold gives up
its content, a table its rows as lines of words. A page or comment that
still does not make a document the allowlist takes is kept as its words.

## What the report lists

The job's report counts the pages, versions, files and comments that came
across, names the people not found here by their address (their work is the
importer's, their name kept as the original author of each version, comment
and file), and lists by page, up to 200 with the count of all of them:

| Kind | What |
|---|---|
| `embed` | frames, videos, scripts and forms, left out |
| `macro` | a block of the other wiki with nothing like it here; its body is kept |
| `externalImage` | a picture from another site, now a link to it |
| `missingFile` | a picture or file the page names and the export lacks |
| `outsideLink` | a link to a page the export lacks, now its words |
| `plainText` | a page or comment no document could hold, kept as its words |
| `label` | a label Stator does not take, or past 50 on a page |
| `unreadPage` | a page file too large or broken to read, left empty |
| `format` | a body in the other wiki's older text format, kept as its words |

Not imported at all, and said once rather than per page: an HTML export
holds no history, so each page starts with one version; an export brings no
permissions or restrictions, so the space has those every new space starts
with; anchors of inline comments, so they are comments below the page;
reactions, watchers and page views.

## Limits

An export is held to an archive's limits: the upload's size, 5000 pages,
100000 versions, 20000 files and 8 GB unpacked; an HTML page file to 8 MB
and `entities.xml` to 1 GB. Past them the upload or the job is refused in a
sentence.
