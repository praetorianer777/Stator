import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, createApi } from "./client";

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("the api client", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("calls the documented path with the cookie and unwraps the body", async () => {
    const fetchMock = vi.fn(async (input: Request) => {
      expect(input.url).toBe("http://app.test/api/v1/healthz");
      expect(input.credentials).toBe("same-origin");
      return json(200, { status: "ok" });
    });
    vi.stubGlobal("fetch", fetchMock);

    const { data } = await createApi("http://app.test/api/v1").GET("/healthz");
    expect(data?.status).toBe("ok");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("turns the error envelope into an ApiError", async () => {
    vi.stubGlobal("fetch", async () => json(401, { error: { code: "unauthenticated", message: "Sign in to continue.", requestId: "r1" } }));

    const failure = await createApi("http://app.test/api/v1")
      .GET("/healthz")
      .catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(ApiError);
    const error = failure as ApiError;
    expect(error.status).toBe(401);
    expect(error.code).toBe("unauthenticated");
    expect(error.message).toBe("Sign in to continue.");
    expect(error.requestId).toBe("r1");
    expect(error.isUnauthenticated).toBe(true);
  });

  it("reports a body that is not JSON as a sentence with the status", async () => {
    vi.stubGlobal("fetch", async () => new Response("<html>bad gateway</html>", { status: 502 }));

    const failure = await createApi("http://app.test/api/v1")
      .GET("/healthz")
      .catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(ApiError);
    expect((failure as ApiError).code).toBe("unexpected_response");
    expect((failure as ApiError).message).toBe("The server answered with status 502. Try again in a moment.");
  });
});
