import { createHash, randomBytes } from "node:crypto";
import type { TestInfo } from "@playwright/test";
import pg from "pg";
import { superuserURL, WEB_URL } from "./stack";

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

/** The cookie the api reads a session from, as config.DefaultSessionCookie names it. */
const SESSION_COOKIE = "stator_session";
const SESSION_HOURS = 1;

const REPLICA_CATCH_UP_MS = 10_000;
const RECHECK_MS = 200;

// The rows went in on the primary, and a session that wrote nothing reads
// from the replica, which would say the organisation lets nobody in yet.
async function untilReplicaLetsIn(secret: string): Promise<void> {
  const deadline = Date.now() + REPLICA_CATCH_UP_MS;
  for (;;) {
    const res = await fetch(`${WEB_URL}/api/v1/spaces`, { headers: { Cookie: `${SESSION_COOKIE}=${secret}` } });
    if (res.ok) return;
    if (Date.now() > deadline)
      throw new Error(`The throwaway person is still refused (${res.status}) after ${REPLICA_CATCH_UP_MS} ms. Is the replica replaying?`);
    await new Promise((resolve) => setTimeout(resolve, RECHECK_MS));
  }
}

/** Somebody signed in to an organisation of their own; call remove when done. */
export interface ThrowawayPerson {
  name: string;
  /** The session secret, to hand a browser as its cookie. */
  session: { name: string; value: string };
  remove: () => Promise<void>;
}

/**
 * A member of a new organisation with a password session, for a spec that
 * changes what belongs to a person and must not change it for alice or bob.
 */
export async function throwawayPerson(testInfo: TestInfo, label: string): Promise<ThrowawayPerson> {
  const tag = `${label}-${testInfo.workerIndex}-${Date.now().toString(36)}`;
  const email = `${tag}@stator.test`;
  const slug = `e2e-${tag}`;
  const secret = randomBytes(32).toString("base64url");
  const name = `Throwaway ${label}`;
  await withDatabase(async (db) => {
    const org = await db.query<{ id: string }>("INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id", [slug, name]);
    const user = await db.query<{ id: string }>("INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id", [email, name]);
    const [orgId, userId] = [org.rows[0]!.id, user.rows[0]!.id];
    await db.query("INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')", [orgId, userId]);
    await db.query(
      `INSERT INTO user_session (user_id, token_hash, current_org_id, proof_org_id, proof, expires_at)
       VALUES ($1, $2, $3, $3, 'password', now() + make_interval(hours => $4))`,
      [userId, createHash("sha256").update(secret).digest(), orgId, SESSION_HOURS],
    );
  });
  await untilReplicaLetsIn(secret);
  return {
    name,
    session: { name: SESSION_COOKIE, value: secret },
    remove: () =>
      withDatabase(async (db) => {
        await db.query("DELETE FROM app_user WHERE email = $1", [email]);
        await db.query("DELETE FROM org WHERE slug = $1", [slug]);
      }),
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
