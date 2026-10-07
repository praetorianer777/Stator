import { crc32 } from "node:zlib";

// Word documents made here, part by part, so the suite carries no files from
// other programs: a zip of stored parts, which is all a .docx needs to be.

const LOCAL_FILE = 0x04034b50;
const DIRECTORY_ENTRY = 0x02014b50;
const END_OF_DIRECTORY = 0x06054b50;
const ZIP_VERSION = 20;

/** A zip of the files given, stored without compression. */
export function zip(files: Record<string, string | Buffer>): Buffer {
  const locals: Buffer[] = [];
  const entries: Buffer[] = [];
  let offset = 0;
  for (const [name, content] of Object.entries(files)) {
    const data = typeof content === "string" ? Buffer.from(content, "utf8") : content;
    const path = Buffer.from(name, "utf8");
    const sum = crc32(data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(LOCAL_FILE, 0);
    local.writeUInt16LE(ZIP_VERSION, 4);
    local.writeUInt32LE(sum, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(path.length, 26);
    locals.push(local, path, data);
    const entry = Buffer.alloc(46);
    entry.writeUInt32LE(DIRECTORY_ENTRY, 0);
    entry.writeUInt16LE(ZIP_VERSION, 4);
    entry.writeUInt16LE(ZIP_VERSION, 6);
    entry.writeUInt32LE(sum, 16);
    entry.writeUInt32LE(data.length, 20);
    entry.writeUInt32LE(data.length, 24);
    entry.writeUInt16LE(path.length, 28);
    entry.writeUInt32LE(offset, 42);
    entries.push(entry, path);
    offset += local.length + path.length + data.length;
  }
  const directory = Buffer.concat(entries);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(END_OF_DIRECTORY, 0);
  const count = Object.keys(files).length;
  end.writeUInt16LE(count, 8);
  end.writeUInt16LE(count, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, directory, end]);
}

const W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";
const R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const REL = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const NS = `xmlns:w="${W}" xmlns:r="${R}" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`;

const xmlText = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

/** A paragraph, in a style when one is named. */
export function para(words: string, style?: string): string {
  const props = style ? `<w:pPr><w:pStyle w:val="${style}"/></w:pPr>` : "";
  return `<w:p>${props}<w:r><w:t xml:space="preserve">${xmlText(words)}</w:t></w:r></w:p>`;
}

/** A bulleted item at a level of the document's one list. */
export function bullet(words: string, level = 0): string {
  return `<w:p><w:pPr><w:numPr><w:ilvl w:val="${level}"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>${xmlText(words)}</w:t></w:r></w:p>`;
}

/** A table whose first row Word repeats as its header. */
export function table(rows: string[][]): string {
  const columns = rows[0] ?? [];
  const cell = (words: string) => `<w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr>${para(words)}</w:tc>`;
  const body = rows.map((row, i) => `<w:tr>${i === 0 ? "<w:trPr><w:tblHeader/></w:trPr>" : ""}${row.map(cell).join("")}</w:tr>`).join("");
  return `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid>${columns.map(() => '<w:gridCol w:w="2000"/>').join("")}</w:tblGrid>${body}</w:tbl>`;
}

/** The document's one picture, as a paragraph of its own with its description. */
export function picture(description: string, widthPx: number, heightPx: number): string {
  const emu = 9525;
  const size = `cx="${widthPx * emu}" cy="${heightPx * emu}"`;
  return (
    `<w:p><w:r><w:drawing><wp:inline><wp:extent ${size}/><wp:docPr id="1" name="Picture 1" descr="${xmlText(description)}"/>` +
    `<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="1" name="Picture 1"/><pic:cNvPicPr/></pic:nvPicPr>` +
    `<pic:blipFill><a:blip r:embed="rIdPicture"/></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext ${size}/></a:xfrm><a:prstGeom prst="rect"/></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
  );
}

const STYLES =
  `<w:styles ${NS}><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
  `<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>` +
  `<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style></w:styles>`;

const NUMBERING =
  `<w:numbering ${NS}><w:abstractNum w:abstractNumId="0">` +
  `<w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>` +
  `<w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/><w:lvlText w:val="◦"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl>` +
  `</w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`;

/** A Word document with the title property and body given, and a PNG when the body shows one. */
export function wordDocument({ title, body, png }: { title: string; body: string; png?: Buffer }): Buffer {
  const rels = [
    `<Relationship Id="rIdStyles" Type="${REL}/styles" Target="styles.xml"/>`,
    `<Relationship Id="rIdNumbering" Type="${REL}/numbering" Target="numbering.xml"/>`,
    png ? `<Relationship Id="rIdPicture" Type="${REL}/image" Target="media/image1.png"/>` : "",
  ].join("");
  const parts: Record<string, string | Buffer> = {
    "[Content_Types].xml":
      '<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">' +
      '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>' +
      '<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>',
    "_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="${REL}/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`,
    "docProps/core.xml": `<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>${xmlText(title)}</dc:title></cp:coreProperties>`,
    "word/document.xml": `<?xml version="1.0"?><w:document ${NS}><w:body>${body}</w:body></w:document>`,
    "word/styles.xml": `<?xml version="1.0"?>${STYLES}`,
    "word/numbering.xml": `<?xml version="1.0"?>${NUMBERING}`,
    "word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${rels}</Relationships>`,
  };
  if (png) parts["word/media/image1.png"] = png;
  return zip(parts);
}

/** What a browser sends a .docx as. */
export const DOCX_TYPE = "application/vnd.openxmlformats-officedocument.wordprocessingml.document";
