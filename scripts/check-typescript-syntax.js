const fs = require('node:fs');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of [
    'typescript',
    path.join(process.cwd(), 'apps/admin-web/node_modules/typescript'),
    path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')
  ]) {
    try { return require(candidate); } catch {}
  }
  console.log('TypeScript is not installed; syntax transpilation check skipped.');
  process.exit(0);
}

const ts = loadTypeScript();
let failed = false;

function walk(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      walk(file);
      continue;
    }
    if (!/\.(ts|tsx)$/.test(entry.name) || /\.d\.ts$/.test(entry.name)) continue;
    const source = fs.readFileSync(file, 'utf8');
    const result = ts.transpileModule(source, {
      fileName: file,
      reportDiagnostics: true,
      compilerOptions: {
        jsx: ts.JsxEmit.ReactJSX,
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.ESNext,
        experimentalDecorators: true,
        emitDecoratorMetadata: true
      }
    });
    for (const diagnostic of result.diagnostics ?? []) {
      if (diagnostic.category !== ts.DiagnosticCategory.Error) continue;
      failed = true;
      console.error(`${file}: ${ts.flattenDiagnosticMessageText(diagnostic.messageText, '\n')}`);
    }
  }
}

walk('apps/admin-web');
walk('services/openwa-gateway/src');
if (failed) process.exit(1);
console.log('TypeScript/TSX syntax transpilation passed.');
