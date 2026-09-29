import createClient, { type Middleware } from "openapi-fetch";
import { API_BASE } from "@/config";
import { t } from "@/i18n";
import type { paths } from "./schema";

/**
 * The one place that talks to the API, typed from the server's own OpenAPI
 * document: a path, a body or a response that does not exist fails to compile.
 */

/** The error envelope every endpoint answers with when it refuses. */
export interface ApiErrorBody {
  code: string;
  message: string;
  fields?: Record<string, string>;
  requestId?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: Record<string, string>;
  readonly requestId?: string;

  constructor(status: number, body: ApiErrorBody) {
    super(body.message);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code;
    this.fields = body.fields ?? {};
    this.requestId = body.requestId;
  }

  /** True when the caller is not signed in, which the router treats specially. */
  get isUnauthenticated(): boolean {
    return this.status === 401;
  }
}

// A non-JSON body from a proxy or gateway is still a failure that has to be
// reported usefully rather than crashing on, so it becomes a generic envelope.
const raiseOnError: Middleware = {
  async onResponse({ response }) {
    if (response.ok) return response;
    let body: { error?: ApiErrorBody } | undefined;
    try {
      body = (await response.clone().json()) as { error?: ApiErrorBody };
    } catch {
      body = undefined;
    }
    throw new ApiError(response.status, body?.error ?? { code: "unexpected_response", message: t.api.unexpected(response.status) });
  },
};

export function createApi(baseUrl: string = API_BASE) {
  // The session lives in an HttpOnly cookie, so it has to be sent explicitly.
  const client = createClient<paths>({ baseUrl, credentials: "same-origin" });
  client.use(raiseOnError);
  return client;
}

/** The one client the application shares. */
export const api = createApi();
