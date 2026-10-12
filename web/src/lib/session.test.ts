import { describe, expect, it } from "vitest";
import { ApiError } from "@/api/client";
import { QUERY_NETWORK_RETRIES } from "@/config";
import { createQueryClient } from "@/lib/session";

describe("the query client", () => {
  it("asks again when a request got no answer, as when the network changes under it", async () => {
    const client = createQueryClient(() => {});
    let calls = 0;
    const data = await client.fetchQuery({
      queryKey: ["dropped"],
      queryFn: async () => {
        calls++;
        if (calls === 1) throw new TypeError("Failed to fetch");
        return "answered";
      },
    });
    expect(data).toBe("answered");
    expect(calls).toBe(2);
  });

  it("gives up after a few tries when no answer ever comes", async () => {
    const client = createQueryClient(() => {});
    let calls = 0;
    await expect(
      client.fetchQuery({
        queryKey: ["gone"],
        queryFn: async () => {
          calls++;
          throw new TypeError("Failed to fetch");
        },
      }),
    ).rejects.toThrow("Failed to fetch");
    expect(calls).toBe(QUERY_NETWORK_RETRIES + 1);
  });

  it("takes an answer as the answer and never asks again", async () => {
    const client = createQueryClient(() => {});
    let calls = 0;
    await expect(
      client.fetchQuery({
        queryKey: ["missing"],
        queryFn: async () => {
          calls++;
          throw new ApiError(404, { code: "not_found", message: "No such page." });
        },
      }),
    ).rejects.toBeInstanceOf(ApiError);
    expect(calls).toBe(1);
  });
});
