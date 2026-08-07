import { createHash } from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const serviceRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const repoRoot = path.resolve(serviceRoot, '../..');
const sourceRoot = path.join(repoRoot, 'third_party/openwa/upstream/src');
const targetRoot = path.join(serviceRoot, 'src/retained-openwa');
const entries = [
  'engine/adapters/whatsapp-web-js.adapter.ts',
  'engine/adapters/baileys.adapter.ts',
];
const EXPECTED_SOURCE_FILES = 56;
const skippedTargets = new Set([
  'engine/identity/lid-mapping-store.service.ts',
  'config/configuration.ts',
]);
const importPattern = /(?:import|export)\s+(?:type\s+)?(?:[^'\"]*?\s+from\s+)?['\"](\.[^'\"]+)['\"]/g;
function relativeSource(file) {
  return path.relative(sourceRoot, file).replaceAll('\\', '/');
}

function resolveRelative(fromFile, specifier) {
  const base = path.resolve(path.dirname(fromFile), specifier);
  for (const candidate of [`${base}.ts`, `${base}.tsx`, path.join(base, 'index.ts')]) {
    if (fs.existsSync(candidate)) return candidate;
  }
  throw new Error(`Unable to resolve retained OpenWA import ${specifier} from ${relativeSource(fromFile)}`);
}

function sha256(text) {
  return createHash('sha256').update(text).digest('hex');
}

function rewriteSource(relative, text) {
  if (relative === 'engine/adapters/whatsapp-web-js.adapter.ts' ||
      relative === 'engine/adapters/baileys-session-store.ts' ||
      relative === 'engine/types/baileys.types.ts') {
    text = text.replaceAll("../identity/lid-mapping-store.service", "../identity/lid-mapping-store");
  }
  if (relative === 'engine/adapters/baileys-session-store.ts') {
    text = text.replace("../../config/configuration", "../../compat/env");
  }
  return text;
}
const seen = new Set();
const queue = [...entries];
const manifest = [];

while (queue.length) {
  const relative = queue.shift();
  if (seen.has(relative)) continue;
  seen.add(relative);
  const sourceFile = path.join(sourceRoot, relative);
  const original = fs.readFileSync(sourceFile, 'utf8');

  for (const match of original.matchAll(importPattern)) {
    const resolved = resolveRelative(sourceFile, match[1]);
    const child = relativeSource(resolved);
    if (skippedTargets.has(child)) continue;
    if (!seen.has(child)) queue.push(child);
  }

  const transformed = rewriteSource(relative, original);
  manifest.push({ path: relative, sourceSha256: sha256(original), generatedSha256: sha256(transformed) });
}

if (seen.size !== EXPECTED_SOURCE_FILES) {
  throw new Error(`Retained OpenWA closure changed: expected ${EXPECTED_SOURCE_FILES} source files, found ${seen.size}. Review the upstream delta before accepting it.`);
}
for (const forbidden of skippedTargets) {
  if (seen.has(forbidden)) throw new Error(`Forbidden application/database source entered retained transport closure: ${forbidden}`);
}

fs.rmSync(targetRoot, { recursive: true, force: true });
fs.mkdirSync(targetRoot, { recursive: true });
for (const item of manifest) {
  const sourceFile = path.join(sourceRoot, item.path);
  const targetFile = path.join(targetRoot, item.path);
  fs.mkdirSync(path.dirname(targetFile), { recursive: true });
  fs.writeFileSync(targetFile, rewriteSource(item.path, fs.readFileSync(sourceFile, 'utf8')));
}

const lidShim = `export interface LidMappingStore {
  getCached(lid: string): string | null | undefined;
  lidsForPhone(phone: string): string[];
  remember(lid: string, phone: string | null, sessionId?: string): Promise<void>;
}
`;
const envShim = `export function resolveNonNegativeIntEnv(raw: string | undefined, fallback: number): number {
  const trimmed = raw?.trim();
  if (!trimmed || !/^\\d+$/.test(trimmed)) return fallback;
  return Number(trimmed);
}
`;
const lidTarget = path.join(targetRoot, 'engine/identity/lid-mapping-store.ts');
const envTarget = path.join(targetRoot, 'compat/env.ts');
fs.mkdirSync(path.dirname(lidTarget), { recursive: true });
fs.mkdirSync(path.dirname(envTarget), { recursive: true });
fs.writeFileSync(lidTarget, lidShim);
fs.writeFileSync(envTarget, envShim);
const generatedFiles = [];
for (const item of manifest) generatedFiles.push(path.join(targetRoot, item.path));
generatedFiles.push(lidTarget, envTarget);
for (const file of generatedFiles) {
  const text = fs.readFileSync(file, 'utf8');
  for (const forbidden of ['@nestjs/typeorm', "from 'typeorm'", 'ServeStaticModule', 'dashboard/dist']) {
    if (text.includes(forbidden)) {
      throw new Error(`Forbidden upstream application dependency ${JSON.stringify(forbidden)} found in ${path.relative(targetRoot, file)}`);
    }
  }
}

const provenance = {
  generatedAt: new Date().toISOString(),
  source: 'third_party/openwa/upstream/src',
  entrypoints: entries,
  sourceFileCount: seen.size,
  transformations: [
    'LidMappingStore reduced to interface-only worker shim; upstream TypeORM service excluded',
    'resolveNonNegativeIntEnv reduced to worker compatibility shim; full upstream configuration excluded',
  ],
  files: manifest.sort((a, b) => a.path.localeCompare(b.path)),
};
fs.writeFileSync(path.join(targetRoot, 'PROVENANCE.json'), `${JSON.stringify(provenance, null, 2)}\n`);
console.log(`Synced ${seen.size} pinned OpenWA transport source files into ${path.relative(repoRoot, targetRoot)}.`);