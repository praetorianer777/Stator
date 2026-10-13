import type { TestInfo } from "@playwright/test";
import pg from "pg";
import { superuserURL } from "./stack";

// Straight into the stack's database, for what no endpoint makes yet. Keep it
// to arranging and tidying up; what a spec checks goes through the app.

/** Runs fn on a connection as the database's superuser, closed afterwards, once the replicas have what it wrote. */
export async function withDatabase<T>(fn: (client: pg.Client) => Promise<T>): Promise<T> {
  const client = new pg.Client({ connectionString: superuserURL() });
  await client.connect();
  try {
    const result = await fn(client);
    await settle(client);
    return result;
  } finally {
    await client.end();
  }
}

/** Waits until every replica has replayed what the primary has written so far. */
export async function settleReplicas(): Promise<void> {
  await withDatabase(async () => {});
}

const SETTLE_MS = 10_000;
const SETTLE_RECHECK_MS = 50;

// No read is pinned to what the superuser wrote, and a read can reach the
// primary once and a replica behind it the next time, so a retry that saw the
// rows proves nothing about the read after it. As the integration suite's
// settle, this waits until every replica has replayed all written so far;
// emitting a flushed message first sends a commit still in the WAL buffers.
async function settle(client: pg.Client): Promise<void> {
  const { rows } = await client.query<{ lsn: string }>("SELECT pg_logical_emit_message(false, 'stator-settle', '', true)::text AS lsn");
  const lsn = rows[0]!.lsn;
  const deadline = Date.now() + SETTLE_MS;
  for (;;) {
    const behind = await client.query("SELECT 1 FROM pg_stat_replication WHERE replay_lsn IS NULL OR replay_lsn < $1::pg_lsn", [lsn]);
    if (behind.rowCount === 0) return;
    if (Date.now() > deadline) throw new Error(`A replica has not replayed ${lsn} after ${SETTLE_MS} ms. Is it streaming from the primary?`);
    await new Promise((resolve) => setTimeout(resolve, SETTLE_RECHECK_MS));
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

/**
 * Makes a page's verification run out a minute ago. The database stamps every
 * verification with the moment it is made, so its triggers are held off.
 */
export async function lapseVerification(pageId: string): Promise<void> {
  await withDatabase(async (db) => {
    await db.query("BEGIN");
    try {
      await db.query("SET LOCAL session_replication_role = replica");
      const { rowCount } = await db.query(
        "UPDATE page_verification SET verified_at = now() - interval '31 days', expires_at = now() - interval '1 minute' WHERE page_id = $1",
        [pageId],
      );
      if (rowCount !== 1) throw new Error(`The page ${pageId} has no verification to make run out.`);
      await db.query("COMMIT");
    } catch (error) {
      await db.query("ROLLBACK");
      throw error;
    }
  });
}

/**
 * Brings a page's scheduled publish due now, as if its time came; the
 * stack's worker looks every second and publishes it.
 */
export async function bringScheduleDue(pageId: string): Promise<void> {
  await withDatabase(async (db) => {
    const { rowCount } = await db.query("UPDATE page_schedule SET publish_at = now() WHERE page_id = $1", [pageId]);
    if (rowCount !== 1) throw new Error(`The page ${pageId} has no scheduled publish to bring due.`);
  });
}

/**
 * Says a page was last published days ago and nobody opened it since. The
 * database stamps every publish with the moment it happens, so its triggers are held off.
 */
export async function ageQuietly(pageId: string, days: number): Promise<void> {
  await withDatabase(async (db) => {
    await db.query("BEGIN");
    try {
      await db.query("SET LOCAL session_replication_role = replica");
      const { rowCount } = await db.query("UPDATE page SET published_at = now() - make_interval(days => $2) WHERE id = $1 AND published_at IS NOT NULL", [
        pageId,
        days,
      ]);
      if (rowCount !== 1) throw new Error(`The page ${pageId} is not published, so it cannot be made old.`);
      await db.query("DELETE FROM page_visit WHERE page_id = $1", [pageId]);
      await db.query("COMMIT");
    } catch (error) {
      await db.query("ROLLBACK");
      throw error;
    }
  });
}
