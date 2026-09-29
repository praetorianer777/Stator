import type { TestInfo } from "@playwright/test";
import pg from "pg";
import { superuserURL } from "./stack";

// Straight into the stack's database, for what no endpoint makes yet. Keep it
// to arranging and tidying up; what a spec checks goes through the app.

/** Runs fn on a connection as the database's superuser, closed afterwards. */
export async function withDatabase<T>(fn: (client: pg.Client) => Promise<T>): Promise<T> {
  const client = new pg.Client({ connectionString: superuserURL() });
  await client.connect();
  try {
    return await fn(client);
  } finally {
    await client.end();
  }
}

/**
 * Nothing listens on port 9 of the api's own container, so asking this issuer
 * who it is fails at once rather than after a timeout.
 */
export const DEAD_ISSUER = "http://127.0.0.1:9/realms/nowhere";

/** An organisation of its own whose provider cannot be reached; call remove when done. */
export async function orgWithDeadProvider(testInfo: TestInfo): Promise<{ slug: string; remove: () => Promise<void> }> {
  const slug = `e2e-dead-${testInfo.workerIndex}-${Date.now().toString(36)}`;
  await withDatabase(async (db) => {
    const { rows } = await db.query<{ id: string }>("INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id", [slug, "Dead provider"]);
    await db.query("INSERT INTO oidc_provider (org_id, issuer, client_id) VALUES ($1, $2, 'stator')", [rows[0]!.id, DEAD_ISSUER]);
  });
  return {
    slug,
    remove: () => withDatabase(async (db) => void (await db.query("DELETE FROM org WHERE slug = $1", [slug]))),
  };
}
