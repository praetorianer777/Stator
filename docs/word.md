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
