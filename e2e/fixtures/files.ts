import { crc32, deflateSync } from "node:zlib";
import { must, type StatorApi } from "./api";

/** A PNG of one colour, made here so the suite carries no binary files. */
export function png(width: number, height: number, [r, g, b]: [number, number, number]): Buffer {
  const row = Buffer.alloc(1 + width * 3);
  for (let x = 0; x < width; x++) row.set([r, g, b], 1 + x * 3);
  const raw = Buffer.concat(Array.from({ length: height }, () => row));
  const chunk = (type: string, data: Buffer) => {
    const length = Buffer.alloc(4);
    length.writeUInt32BE(data.length);
    const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc32(body));
    return Buffer.concat([length, body, sum]);
  };
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header.set([8, 2, 0, 0, 0], 8);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

/** Uploads bytes to a page as a file of the given name and type, and answers its id. */
export async function upload(api: StatorApi, pageId: string, name: string, type: string, bytes: Buffer): Promise<string> {
  const form = new FormData();
  form.append("file", new Blob([new Uint8Array(bytes)], { type }), name);
  // The generated type describes the multipart fields as strings; the form itself is what is sent.
  const body = form as unknown as { file: string };
  return must(await api.POST("/pages/{pageID}/attachments", { params: { path: { pageID: pageId } }, body })).attachment.id;
}
