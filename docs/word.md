# Word export

A published page is exported as a Word document (`.docx`) for editing
offline. The api writes it from the page's document (`backend/internal/docx`),
as the reader may read the page; `docs/decisions.md` says why it is written
natively rather than converted.

## Operations

| Operation | Who | Answers |
|---|---|---|
| `GET /pages/{pageID}/docx` | a member who may view the page | the page as the member reads it; audited as `page.exported` with scope `docx` |
| `GET /public/{orgSlug}/pages/{pageID}/docx` | anybody, for a page anybody may read | the page as anybody reads it, six a minute per page from one address |
| `GET /public/{orgSlug}/links/{token}/docx` | whoever holds a public link | the page the link opens, without the link in it |

A folder and a page never published are refused with 409 `not_exportable`.
Pictures past 50 MB in all are refused with 422 `docx_too_large`. The file is
named after the space, the page and the day: `DOCS-setting-up-2026-10-07.docx`.

## The document

The title in Word's Title style, a line with the space, the version and its
day and a link to the page, then the page. The document's properties carry
the title, the space as its subject, who made the page and who changed it
last (left out for anybody), the version as its revision, the language and
the days it was made and changed. Words the export adds, and dates, are in
the reader's language, English or German, and Word checks spelling in it.

## Blocks

| Node | In Word |
|---|---|
| `paragraph` | a paragraph |
| `heading` | Word's Heading 1 to 3, bookmarked, so the navigation pane, links within the page and the contents find it |
| `bulletList`, `orderedList`, `listItem` | Word numbering, one level in for each list inside another; an ordered list counts from its start in its own style (1, a, A, i, I) |
| `taskList`, `taskItem` | a check box character, ticked or not, before each item |
| `blockquote` | a box with a grey bar on its left |
| `codeBlock` | the Code style: monospace on a light grey background, its lines and spaces kept |
| `horizontalRule` | a paragraph with a line beneath |
| `table`, `tableRow`, `tableCell`, `tableHeader` | a table with its column widths, merged cells, header cells shaded and bold, a header row repeated on each sheet, and cell colours and alignment |
| `panel` | a box in the panel's colour with its kind (Info, Warning, ...) as its first line |
| `expand` | a box with its title as its first line and everything in it, open |
| `columns`, `column` | a table without borders, each column its share of the row |
| `decision` | its line after Decided or Undecided in colour |
| `mathBlock` | its TeX source, centred in the Formula style |
| `diagram` | a sentence saying Stator draws it, then its Mermaid source in the Code style |
| `sketch` | a sentence saying Stator draws it, with its title; Word would need a raster of the drawing, which the server does not draw |
| `linkCard` | its address as a link |
| `excerpt` | the blocks it marks |
| `include` | for a member, what the include shows them in a box under a link to its page, or a sentence when they may not read it; for anybody, a link to the page; for a link's holder, a sentence |
| `properties`, `propertyRow` | a two-column table, the names shaded |
| `tableChart` | a sentence naming the chart, then its table |
| `image` | the picture inside the document, as wide as stored and no wider than the text, its description kept; a sentence when the reader may not open it |
| `gallery`, `galleryImage` | its pictures in rows of its columns, each with its caption |
| `tableOfContents` | the page's headings to its level, each a link to its heading |
| `propertiesReport`, `labelledPages`, `recentlyUpdated`, `blogPosts`, `taskReport`, `attachmentList`, `calendar`, `templateButton`, `contributors`, `childPages` | a sentence saying what it lists, which the page in Stator shows as it is now |
| `armatureIssueBlock`, `armatureIssueList`, `armatureChart`, `armatureRoadmap` | a sentence with the issue's key or the query, which Armature answers for each reader |

## Inline

| Node or mark | In Word |
|---|---|
| `text` | a run |
| `bold`, `italic`, `strike` | bold, italic, struck through |
| `code` | the Inline Code style: monospace, shaded |
| `link` | a hyperlink; a link within Stator is made absolute, one to a heading of the page goes to its bookmark |
| `hardBreak` | a line break |
| `mention` | @ and the name, in the accent colour; for anybody the name as written |
| `attachment` | the file's name, linked to its download where the reader may download it |
| `armatureIssue` | the issue's key in bold |
| `status` | its label in capitals, in its colour |
| `date` | the day in words, in the reader's language |
| `mathInline` | its TeX source in the Formula style |
| `hint`, `inlineComment` | their words alone: a template's placeholder is never published, and a passage's thread stays in Stator |

# Word import

Word documents (`.docx`) become pages under a page, read natively by the
api (`backend/internal/docx`, the reading half) and made through the page
and file services as the person importing (`backend/internal/wordio`).
`docs/decisions.md` says why.

## Operations

| Operation | Who | Answers |
|---|---|---|
| `POST /pages/{pageID}/import/docx` | whoever may add pages under the page and edit it | 201 with the page made and the warnings, at once |
| `POST /pages/{pageID}/word-imports` | the same | 202 with an import the worker runs: up to 50 documents, or `.zip` archives of them |
| `GET /word-imports/{importID}` | whoever queued it | how many pages it made of how many, and for each file the page with its warnings, or why it made none |

A page made is published, as a Markdown import publishes its pages, titled
by the document's title property, else its Title paragraph, else its one
leading heading of level 1 (which then leaves the body, the headings below
moving up a level), else its file name. A document's pictures are files of
its page. In an archive each folder is a page holding its documents, or,
when a document beside it has its name, that document's page holds them.
Files that are no Word documents are listed as left out.

Refusals, each a sentence naming the file: a document over 50 MB, pictures
over 50 MB in one document, a part unpacking to more than 32 MB, an upload
over 200 MB or of more than 50 documents or 200 pages, a Word 97-2003
document or one protected by a password, and anything that is no Word
document. In an import of several, a document that cannot be read is
reported and passed over; the others are made.

## Blocks

| In Word | On the page |
|---|---|
| Title paragraph | the page's title, when the properties name none |
| a paragraph whose style or own setting has an outline level (Heading 1 to 9) | a heading; levels below 3 become 3, said once |
| a paragraph | a paragraph; an empty one is left out |
| numbered and bulleted paragraphs (`numbering.xml`) | ordered and bullet lists, nested by level; an ordered list keeps its counting style and start, and counts on across a break in it; a paragraph set in as far as an item's text stays in the item |
| a paragraph starting with a check box (☐, ☑, ☒) | a task, ticked or not |
| a code style (Code, Source Code, HTML Preformatted, Plain Text) or a paragraph all in a monospaced font | a code block; consecutive ones join, tabs and spaces kept |
| Quote and Intense Quote | a quote; consecutive ones join |
| a paragraph with a line beneath and nothing in it | a horizontal rule |
| a table | a table: a header row where Word repeats it or its words are all bold, cells merged across (`gridSpan`) and down (`vMerge`), the fills the editor offers and a shared alignment |
| a table of one cell | a quote around its blocks, or a panel when it has a panel's fill |
| a picture (inline or anchored, DrawingML or VML) | a picture of the page, a file of it, its description kept; its width where it is shown smaller or larger than it is |
| a table of contents (a field or a contents control) | a table of contents |
| footnotes and endnotes | the reference as its number in brackets, the notes as a numbered list at the end of the page under a rule |
| a text box | its text where it is anchored, said once |
| Stator's own Formula style | a formula |

## Inline

| In Word | On the page |
|---|---|
| bold, italic, strike and double strike, by the run or its character style | bold, italic, strike |
| a monospaced font or a code character style | code |
| a hyperlink, or a HYPERLINK field | a link, if it is a web or mail address; one to a heading's bookmark goes to the heading |
| a line break | a line break; page and column breaks are left out |
| a tab | a space, or a tab in code |
| a footnote reference | its number in brackets |

## Said and left out

Each of these is a warning, once per document: underline and superscript or
subscript, which a page has no mark for (their words kept); hidden text;
comments (counted); tracked changes, accepted as the document reads with
every change made; headers and footers; charts, SmartArt and shapes without
text; embedded objects; equations, kept as their text; pictures in a format
browsers do not show, such as EMF, or larger than a file may be; pictures
linked from outside the document; links that are not web or mail addresses,
and links to bookmarks that are no heading's; symbols from a symbol font;
parts kept in another format. Fonts, colours, sizes, spacing, alignment of
paragraphs and page layout follow the page's theme and are not reported.

A document Stator exported (its properties name Stator) comes back as the
page it was for the blocks Word carries: the line under its title is left
out, panels come back by their fill, and the unit tests hold the round trip.
