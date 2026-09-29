import type { StatorApi } from "./api";

/** An organisation made for one spec file alone. */
export interface FreshOrg {
  slug: string;
  id: string;
}

/**
 * Makes a throwaway organisation so a spec's data never meets another's.
 * A stub until the api can make one; until then specs share the seeded `demo`.
 */
export async function freshOrg(_api: StatorApi, _label: string): Promise<FreshOrg> {
  throw new Error(
    "freshOrg needs an endpoint that makes a throwaway organisation with alice and bob in it, and one that removes it. " +
      "Until the api offers them, use the seeded demo organisation and clean up what the spec made.",
  );
}
