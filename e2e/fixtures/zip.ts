import { crc32, inflateRawSync } from "node:zlib";

// Just enough of the zip format to read what an export wrote: the central
// directory names every entry, its method and where its local header is.
const END_OF_DIRECTORY = 0x06054b50;
const DIRECTORY_ENTRY = 0x02014b50;
const END_RECORD_SIZE = 22;
const DIRECTORY_ENTRY_SIZE = 46;
const LOCAL_HEADER_SIZE = 30;
const STORED = 0;
const DEFLATED = 8;

/** Every file of a zip archive by its path, unpacked. */
export function unzip(archive: Buffer): Map<string, Buffer> {
  let end = archive.length - END_RECORD_SIZE;
  while (end >= 0 && archive.readUInt32LE(end) !== END_OF_DIRECTORY) end--;
  if (end < 0) throw new Error("the file is no zip archive");
  const count = archive.readUInt16LE(end + 10);
  let at = archive.readUInt32LE(end + 16);
  const out = new Map<string, Buffer>();
  for (let i = 0; i < count; i++) {
    if (archive.readUInt32LE(at) !== DIRECTORY_ENTRY) throw new Error("the zip's directory is broken");
    const method = archive.readUInt16LE(at + 10);
    const compressed = archive.readUInt32LE(at + 20);
    const nameLength = archive.readUInt16LE(at + 28);
    const extraLength = archive.readUInt16LE(at + 30);
    const commentLength = archive.readUInt16LE(at + 32);
    const local = archive.readUInt32LE(at + 42);
    const name = archive.subarray(at + DIRECTORY_ENTRY_SIZE, at + DIRECTORY_ENTRY_SIZE + nameLength).toString("utf8");
    const start = local + LOCAL_HEADER_SIZE + archive.readUInt16LE(local + 26) + archive.readUInt16LE(local + 28);
    const data = archive.subarray(start, start + compressed);
    if (method === STORED) out.set(name, Buffer.from(data));
    else if (method === DEFLATED) out.set(name, inflateRawSync(data));
    else throw new Error(`the zip packs ${name} in a way this reader does not know`);
    at += DIRECTORY_ENTRY_SIZE + nameLength + extraLength + commentLength;
  }
  return out;
}

const LOCAL_HEADER = 0x04034b50;
const ZIP_VERSION = 20;
const UTF8_NAMES = 0x0800;
// 1980-01-01, the first day a zip can name.
const FIRST_DAY = 0x21;

/** A zip of the files given by path, stored as they are, as the exports a test builds need. */
export function zip(files: Record<string, Buffer | string>): Buffer {
  const parts: Buffer[] = [];
  const directory: Buffer[] = [];
  let offset = 0;
  for (const [path, content] of Object.entries(files)) {
    const data = typeof content === "string" ? Buffer.from(content, "utf8") : content;
    const name = Buffer.from(path, "utf8");
    const sum = crc32(data);
    const local = Buffer.alloc(LOCAL_HEADER_SIZE);
    local.writeUInt32LE(LOCAL_HEADER, 0);
    local.writeUInt16LE(ZIP_VERSION, 4);
    local.writeUInt16LE(UTF8_NAMES, 6);
    local.writeUInt16LE(STORED, 8);
    local.writeUInt16LE(FIRST_DAY, 12);
    local.writeUInt32LE(sum, 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(name.length, 26);
    const entry = Buffer.alloc(DIRECTORY_ENTRY_SIZE);
    entry.writeUInt32LE(DIRECTORY_ENTRY, 0);
    entry.writeUInt16LE(ZIP_VERSION, 4);
    entry.writeUInt16LE(ZIP_VERSION, 6);
    entry.writeUInt16LE(UTF8_NAMES, 8);
    entry.writeUInt16LE(STORED, 10);
    entry.writeUInt16LE(FIRST_DAY, 14);
    entry.writeUInt32LE(sum, 16);
    entry.writeUInt32LE(data.length, 20);
    entry.writeUInt32LE(data.length, 24);
    entry.writeUInt16LE(name.length, 28);
    entry.writeUInt32LE(offset, 42);
    parts.push(local, name, data);
    directory.push(entry, name);
    offset += local.length + name.length + data.length;
  }
  const size = directory.reduce((n, b) => n + b.length, 0);
  const count = Object.keys(files).length;
  const end = Buffer.alloc(END_RECORD_SIZE);
  end.writeUInt32LE(END_OF_DIRECTORY, 0);
  end.writeUInt16LE(count, 8);
  end.writeUInt16LE(count, 10);
  end.writeUInt32LE(size, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...parts, ...directory, end]);
}
