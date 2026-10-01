# Markdown import and export

Pages move in and out as Markdown so documentation can live in a repository
and in the wiki. The conversion runs in the API (`backend/internal/markdown`),
the archive and folder handling beside it (`backend/internal/mdio`), and the
page menu's Export as Markdown and Import Markdown call the same operations a
script with a personal access token calls. `docs/decisions.md` says why.

## Operations

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/markdown` | view | the page as one `.md` file |
| `GET /pages/{pageID}/export?subtree` | view, per page | a `.zip` of the page, its files and, with `subtree=true`, every page below it the caller may view |
| `POST /pages/{pageID}/import` | edit | 201 `{pages, warnings}`: the pages made under the page, in reading order |
| `PUT /pages/{pageID}/markdown?version` | edit | `{page, warnings}`: one file published as the next version; 409 `conflict` when `version` is not the page's |

Import and replace take multipart parts named `file`. Each part's file name
is its path in the folder it came from (`docs/guide/setup.md`), which is how a
browser's folder picker and `curl -F "file=@setup.md;filename=docs/guide/setup.md"`
send it; a part may also be a `.zip`, which is unpacked first.

## Layout of an archive

```
guide.md                  the page, its title as the one level 1 heading
guide.files/chart.png     the files on the page
guide/setup.md            a page below it
guide/setup.files/...     its files
guide/setup/...           the pages below that
```

Each name is the page's title as an address slug, numbered when two
siblings would share one. Links between exported pages and to their files are
relative paths, so the archive reads well in a repository and imports back as
the same tree. Siblings keep their order: an archive lists them in tree order,
and an import makes pages in the order their files arrive.

## Import rules

- Every `.md` or `.markdown` file is a page. A folder that holds Markdown is a
  page too, under which its files go; its `index.md`, else its `README.md`,
  is its content, and a folder beside a file of the same name (`guide/` beside
  `guide.md`) holds that page's children. A folder `guide.files` beside
  `guide.md` holds files, never pages.
- The title is the file's level 1 heading when the file opens with it and has
  no other; the headings below move up a level. Otherwise the title is the
  file or folder name, dashes and underscores as spaces, and headings keep
  their level. Levels below what a page has (4 to 6) become level 3.
- A picture or link to a file of the upload becomes a file on the page that
  shows it. A link to another Markdown file of the upload becomes a link to
  the page it made, its `#fragment` kept. A relative link to anything else
  keeps its words and loses the link, with a warning.
- Hidden files and folders (`.git`, `.DS_Store`) and `__MACOSX` are skipped. A
  path that leaves its folder (`../x.md`) refuses the whole upload.
- Every page is made unpublished, its files attached and its content checked
  by the allowlist, and only then published as version 1. A failure part way
  moves what was made to the trash, so nobody else ever sees half an import.
- Limits, as named constants: one import takes `mdio.DefaultMaxImportBytes`
  (100 MB), `mdio.MaxImportFiles` (1000) files and `mdio.MaxImportPages`
  (200) pages; one Markdown file is at most `markdown.MaxSourceBytes` (1 MB)
  and its page at most `document.MaxBytes`; each file at most the upload
  limit (`STATOR_UPLOAD_LIMIT`). A file whose lists and quotes nest deeper
  than a page may, or whose lines are long and full of brackets (which the
  parser takes quadratic time over), is refused before it is parsed. An
  archive is held to the byte limit as it unpacks, whatever its directory
  claims.

## How each node is written

Markdown is CommonMark with the GitHub extensions for tables, task lists and
strikethrough, read by goldmark. What Markdown has a syntax for uses it. What
it has none for is written as an HTML element that reads as its words in any
renderer and is read back as its node: `span` inline and `div` as a block,
each marked `data-stator`. Nothing else in HTML reaches a page.

| Node or mark | Markdown | Back in |
|---|---|---|
| title | `# Title` on the first line | the title |
| heading 1 to 3 | `##` to `####` | same level |
| paragraph, hard break | text, `\` at the end of a line | same |
| bold, italic, strike, code | `**b**`, `*i*`, `~~s~~`, `` `c` `` | same |
| link | `[words](href "title")` | same; a page's link to another exported page is a relative path |
| bullet, numbered list | `-`, `1.` with its start | same |
| task list | `- [x]`, `- [ ]` | same, when every item has a box |
| blockquote | `>` | same |
| code block | fenced, with its language | same |
| horizontal rule | `---` | same |
| table | GFM table, alignment per column | same |
| panel | an alert quote: info `[!NOTE]`, note `[!IMPORTANT]`, success `[!TIP]`, warning `[!WARNING]`, error `[!CAUTION]` | the panel of that kind |
| expand | `<details><summary>Title</summary>` ... `</details>`, the content as Markdown between | expand with its title |
| image | `![alt](page.files/name)` | an image on the page's file |
| image with a width | `<img src="page.files/name" alt="alt" width="320">` | same, width kept |
| file chip | `[name](page.files/name)` | the chip; a link with other words stays a link to the file |
| mention | `<span data-stator="mention" data-id="...">@Name</span>` | the mention |
| status | `<span data-stator="status" data-color="success">DONE</span>` | the status |
| date | `<span data-stator="date">2026-10-01</span>` | the date |
| Armature issue chip | `<span data-stator="issue">KEY-1</span>` | the chip |
| Armature issue block | `<div data-stator="issue">KEY-1</div>` | the block |
| Armature issue list | `<div data-stator="issues" data-columns="key,summary" data-limit="20">query</div>` | the list |
| table of contents | `<div data-stator="toc" data-max-level="3"></div>` | the block |
| child pages | `<div data-stator="child-pages" data-scope="subtree" data-depth="2" data-sort="title"></div>` | the block |

Two lists of one kind in a row are kept apart by an empty comment, `<!-- -->`,
since Markdown would join them.

### What does not come back as it left

These are written so they still read well, and lose the part Markdown cannot
hold:

- A numbered list's letter or roman style is written as `1.` numbering.
- A table's first row is always its header row. A merged cell's content goes
  in its first place and the places it covered stay empty; cell colours and
  column widths are left out; a cell's blocks share one line, joined by
  `<br>`, and blocks other than text and pictures keep only their words.
- A heading's anchor is worked out again from its words, as the editor does.
- A link's `target` and `rel` are left out; the reader sets them.
- A template's hint mark and an inline comment's passage mark are left out,
  their words kept.
- A picture or file the page no longer has is written as its words.

### Reading Markdown from elsewhere

- Autolinks `<https://...>` and `<a@b.c>` are links; bare addresses stay text.
- A picture from elsewhere becomes a link to it, since a page shows only its
  own files; nothing is fetched.
- An HTML block that is not one of the forms above is kept as its source in a
  code block marked `html`, where it shows and never runs. An unknown inline
  tag is left out and its words kept; `<br>` is a line break.
- A link or picture whose address is not a web, mail or site address
  (`javascript:`, `data:`, `//host`) keeps its words only.
- A `data-stator` element whose values the allowlist refuses (a mention that
  is not an id, a colour that is not a theme role, a day that does not exist)
  keeps its words as text.
- What did not come across as written is listed in the answer's `warnings`,
  a sentence each with the file it is about.
