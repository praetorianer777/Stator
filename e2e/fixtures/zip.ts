import { inflateRawSync } from "node:zlib";

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
