import { strict as assert } from "node:assert";
import { readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

test("frontend modules have distinct import names on case-insensitive filesystems", async () => {
  const roots = [new URL("./", import.meta.url), new URL("../bindings/", import.meta.url)];
  const names = new Map<string, string>();
  const conflicts: string[] = [];
  async function visit(directory: string) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const file = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        await visit(file);
      } else if (
        /\.(?:[cm]?[jt]s|[jt]sx)$/.test(entry.name) &&
        !/\.d\.[cm]?ts$/.test(entry.name)
      ) {
        // Extensionless imports can choose .ts before .tsx. Different casing
        // of the same stem is therefore unsafe even with different extensions.
        const key = file.replace(/\.[^.]+$/, "").toLowerCase();
        const previous = names.get(key);
        if (previous) conflicts.push(`${previous} and ${file}`);
        else names.set(key, file);
      }
    }
  }
  for (const root of roots) await visit(fileURLToPath(root));
  assert.deepEqual(
    conflicts.sort(),
    [],
    "Rename conflicting modules so imports resolve consistently on macOS, Windows and Linux",
  );
});
