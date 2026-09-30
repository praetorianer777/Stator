import { vi } from "vitest";

/** One upload the page sent, which the test answers when it likes. */
export interface SentUpload {
  method: string;
  url: string;
  file: File;
  /** Reports that this much of the body has gone. */
  progress: (loaded: number, total: number) => void;
  respond: (status: number, body?: unknown) => void;
  /** The connection breaks before any answer. */
  fail: () => void;
}

/** The upload sent in the given place, or a failure that says it never went. */
export function upload(sent: SentUpload[], index: number): SentUpload {
  const found = sent[index];
  if (!found) throw new Error(`Upload ${index + 1} was never sent; ${sent.length} were.`);
  return found;
}

/**
 * Stands in for XMLHttpRequest, which uploads use for their progress and
 * which the fetch stub does not reach. Each request waits to be answered.
 */
export function stubUploads(): SentUpload[] {
  const sent: SentUpload[] = [];
  class FakeXhr {
    status = 0;
    responseText = "";
    withCredentials = false;
    upload: { onprogress: ((event: ProgressEvent) => void) | null } = { onprogress: null };
    onload: (() => void) | null = null;
    onerror: (() => void) | null = null;
    private method = "";
    private url = "";
    open(method: string, url: string) {
      this.method = method;
      this.url = url;
    }
    send(body: FormData) {
      sent.push({
        method: this.method,
        url: this.url,
        file: body.get("file") as File,
        progress: (loaded, total) => this.upload.onprogress?.({ lengthComputable: true, loaded, total } as ProgressEvent),
        respond: (status, answer) => {
          this.status = status;
          this.responseText = answer === undefined ? "" : JSON.stringify(answer);
          this.onload?.();
        },
        fail: () => this.onerror?.(),
      });
    }
  }
  vi.stubGlobal("XMLHttpRequest", FakeXhr);
  return sent;
}
