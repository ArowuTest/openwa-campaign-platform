import { closeSync, constants, openSync, readFileSync, statSync } from 'node:fs';
import { dirname, isAbsolute, normalize } from 'node:path';

const maximumSecretBytes = 1 << 20;

export function resolveSecretFiles(names: readonly string[]): void {
  const deployed = ['production', 'staging'].includes(String(process.env.NODE_ENV ?? '').trim().toLowerCase());
  for (const name of names) {
    const inline = String(process.env[name] ?? '');
    const fileName = `${name}_FILE`;
    const path = String(process.env[fileName] ?? '').trim();
    if (inline && path) throw new Error(`${name} and ${fileName} cannot both be configured`);
    if (!path) continue;
    if (deployed && !isAbsolute(path)) throw new Error(`${fileName} must be absolute in deployed environments`);
    const stat = statSync(path);
    if (!stat.isFile()) throw new Error(`${fileName} must reference a regular file`);
    if (stat.size > maximumSecretBytes) throw new Error(`${fileName} exceeds the maximum secret size`);
    if (deployed && (stat.mode & 0o077) !== 0 && !readOnlyRuntimeSecret(path)) {
      throw new Error(`${fileName} must not be accessible by group or other users`);
    }
    const value = readFileSync(path, 'utf8').replace(/[\r\n]+$/u, '');
    if (!value.trim()) throw new Error(`${fileName} is empty`);
    if (value.includes('\0')) throw new Error(`${fileName} contains a NUL byte`);
    process.env[name] = value;
  }
}
function readOnlyRuntimeSecret(path: string): boolean {
  const clean = normalize(path);
  if (dirname(clean) !== '/run/secrets') return false;
  try {
    const descriptor = openSync(clean, constants.O_WRONLY);
    closeSync(descriptor);
    return false;
  } catch (error) {
    const code = (error as NodeJS.ErrnoException)?.code;
    return code === 'EACCES' || code === 'EPERM' || code === 'EROFS';
  }
}
