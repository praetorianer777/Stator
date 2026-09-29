import { afterEach, describe, expect, it, vi } from "vitest";
import { stubUploads, upload } from "@/test/xhr";
import { ApiError } from "./client";
import { attachmentUrl, canPreview, formatSize, isImage, uploadAttachment, uploadErrorMessage, type Attachment } from "./attachments";

afterEach(() => vi.unstubAllGlobals());

const pageId = "0195f000-0000-7000-8000-000000000001";
const aFile = (name = "notes.txt", size = 12, type = "text/plain") => new File(["x".repeat(size)], name, { type });
const stored: Attachment = {
  id: "0195f000-0000-7000-8000-0000000000f1",
  pageId,
  fileName: "notes.txt",
  contentType: "text/plain",
  size: 12,
  width: null,
  height: null,
  uploadedByName: "Ada Lovelace",
  createdAt: "2026-09-29T09:00:00Z",
};

describe("formatSize", () => {
  it("reads like a person would say it", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(512)).toBe("512 B");
    expect(formatSize(12_288)).toBe("12 KB");
    expect(formatSize(3.4 * 1024 * 1024)).toBe("3.4 MB");
    expect(formatSize(50 * 1024 * 1024)).toBe("50 MB");
  });
});

describe("what a browser shows in place", () => {
  it("previews exactly what the API serves inline", () => {
    for (const type of ["image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain; charset=utf-8"])
      expect(canPreview(type), type).toBe(true);
    for (const type of ["image/svg+xml", "text/html", "application/zip"]) expect(canPreview(type), type).toBe(false);
    expect(isImage("image/png")).toBe(true);
    expect(isImage("image/svg+xml")).toBe(false);
    expect(isImage("application/pdf")).toBe(false);
  });

  it("links to the bytes, in place only when asked", () => {
    expect(attachmentUrl(stored.id)).toBe(`/api/v1/attachments/${stored.id}`);
    expect(attachmentUrl(stored.id, true)).toBe(`/api/v1/attachments/${stored.id}?inline=1`);
  });
});

describe("uploading a file", () => {
  it("sends it as the one part named file and reports how far it has come", async () => {
    const sent = stubUploads();
    const progress: number[] = [];
    const done = uploadAttachment(pageId, aFile(), (fraction) => progress.push(fraction));
    expect(sent).toHaveLength(1);
    expect(upload(sent, 0).method).toBe("POST");
    expect(upload(sent, 0).url).toBe(`/api/v1/pages/${pageId}/attachments`);
    expect(upload(sent, 0).file.name).toBe("notes.txt");
    upload(sent, 0).progress(6, 12);
    upload(sent, 0).progress(12, 12);
    upload(sent, 0).respond(201, { attachment: stored });
    expect(await done).toEqual(stored);
    expect(progress).toEqual([0.5, 1]);
  });

  it("turns a refusal for size into a sentence that names the limit and what to do", async () => {
    const sent = stubUploads();
    const file = aFile("huge.zip", 2048, "application/zip");
    const done = uploadAttachment(pageId, file);
    upload(sent, 0).respond(413, { error: { code: "too_large", message: "A file may be at most 50 MB." } });
    const error = await done.catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).code).toBe("too_large");
    expect(uploadErrorMessage(file, error)).toBe(
      "huge.zip (2.0 KB) is larger than this site accepts. A file may be at most 50 MB. Make the file smaller, or split it into parts, and attach it again.",
    );
  });

  it("still says what to do when a proxy refuses the size without the API's envelope", async () => {
    const sent = stubUploads();
    const file = aFile("huge.zip", 2048);
    const done = uploadAttachment(pageId, file);
    upload(sent, 0).respond(413);
    expect(uploadErrorMessage(file, await done.catch((e: unknown) => e))).toBe(
      "huge.zip (2.0 KB) is larger than this site accepts. Make the file smaller, or split it into parts, and attach it again.",
    );
  });

  it("names the field the API refused, and a broken connection", async () => {
    const sent = stubUploads();
    const empty = aFile("empty.txt", 0);
    const first = uploadAttachment(pageId, empty);
    upload(sent, 0).respond(422, {
      error: { code: "validation_failed", message: "Check the fields.", fields: { file: "The file is empty. Choose a file with something in it." } },
    });
    expect(uploadErrorMessage(empty, await first.catch((e: unknown) => e))).toBe(
      "empty.txt could not be attached: The file is empty. Choose a file with something in it.",
    );
    const second = uploadAttachment(pageId, aFile());
    upload(sent, 1).fail();
    expect(uploadErrorMessage(aFile(), await second.catch((e: unknown) => e))).toBe(
      "The upload of notes.txt broke off. Check your connection and attach the file again.",
    );
  });
});
