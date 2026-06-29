import { cp, mkdir, readdir, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = path.join(root, "web", "dist");
const target = path.join(root, "internal", "api", "webdist");

await mkdir(target, { recursive: true });

for (const entry of await readdir(target)) {
  if (entry === ".gitkeep" || entry === ".gitignore") continue;
  await rm(path.join(target, entry), { recursive: true, force: true });
}

await cp(source, target, { recursive: true });

console.log(`staged ${path.relative(root, source)} -> ${path.relative(root, target)}`);
