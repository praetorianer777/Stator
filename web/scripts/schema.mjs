// Generates src/api/schema.d.ts from api/openapi.json, or with --check fails
// when the committed copy no longer matches the document.
//
// Until the server publishes api/openapi.json, generation falls back to
// openapi.placeholder.json and the check has nothing to compare against.
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const web = fileURLToPath(new URL("..", import.meta.url));
const documentPath = join(web, "..", "api", "openapi.json");
const placeholderPath = join(web, "openapi.placeholder.json");
const schemaPath = join(web, "src", "api", "schema.d.ts");
const generator = join(web, "node_modules", ".bin", "openapi-typescript");

function generate(from, to) {
  execFileSync(generator, [from, "-o", to], { stdio: ["ignore", "ignore", "inherit"] });
}

const check = process.argv.includes("--check");
const haveDocument = existsSync(documentPath);

if (!check) {
  generate(haveDocument ? documentPath : placeholderPath, schemaPath);
  console.log(`schema.d.ts generated from ${haveDocument ? "api/openapi.json" : "openapi.placeholder.json"}`);
  process.exit(0);
}

if (!haveDocument) {
  console.log("api/openapi.json does not exist yet, so the schema check is skipped.");
  process.exit(0);
}

const scratch = mkdtempSync(join(tmpdir(), "stator-schema-"));
try {
  const fresh = join(scratch, "schema.d.ts");
  generate(documentPath, fresh);
  const committed = existsSync(schemaPath) ? readFileSync(schemaPath, "utf8") : "";
  if (readFileSync(fresh, "utf8") !== committed) {
    console.error("web/src/api/schema.d.ts is out of date with api/openapi.json. Run `make web-schema` and commit the result.");
    process.exit(1);
  }
  console.log("schema.d.ts matches api/openapi.json");
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
