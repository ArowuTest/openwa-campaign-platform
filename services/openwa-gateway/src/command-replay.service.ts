import { Injectable, OnModuleInit, ServiceUnavailableException, UnauthorizedException } from '@nestjs/common';
import { createHash } from 'node:crypto';
import { mkdir, open, readdir, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';

type NonceRecord = {
  nonceHash: string;
  requestHash: string;
  seenAt: number;
  expiresAt: number;
};

@Injectable()
export class CommandReplayService implements OnModuleInit {
  private readonly directory = String(process.env.GATEWAY_COMMAND_NONCE_DIR ?? '/data/gateway-command-nonces');
  private readonly maximumEntries = boundedInteger(process.env.GATEWAY_COMMAND_NONCE_MAX_ENTRIES, 1_000_000, 1_000, 10_000_000);
  private readonly cleanupIntervalMs = boundedInteger(process.env.GATEWAY_COMMAND_NONCE_CLEANUP_INTERVAL_MS, 60_000, 5_000, 3_600_000);
  private lastCleanup = 0;

  async onModuleInit(): Promise<void> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    await this.cleanup(Date.now());
  }

  async claim(nonce: string, requestHash: string, seenAtSeconds: number, expiresAtSeconds: number): Promise<void> {
    const now = Date.now();
    if (now - this.lastCleanup >= this.cleanupIntervalMs) await this.cleanup(now);
    const entries = await readdir(this.directory);
    if (entries.length >= this.maximumEntries) throw new ServiceUnavailableException('gateway command replay store capacity is exhausted');

    const nonceHash = createHash('sha256').update(nonce).digest('hex');
    const record: NonceRecord = { nonceHash, requestHash, seenAt: seenAtSeconds, expiresAt: expiresAtSeconds };
    const path = join(this.directory, `${nonceHash}.json`);
    let handle;
    let created = false;
    try {
      handle = await open(path, 'wx', 0o600);
      await handle.writeFile(JSON.stringify(record), { encoding: 'utf8' });
      await handle.sync();
      created = true;
    } catch (error) {
      if ((error as NodeJS.ErrnoException)?.code === 'EEXIST') throw new UnauthorizedException('gateway command nonce was already used');
      throw new ServiceUnavailableException('gateway command replay evidence could not be persisted');
    } finally {
      await handle?.close().catch(() => undefined);
    }
    if (created) await syncDirectory(this.directory);
  }

  private async cleanup(nowMilliseconds: number): Promise<void> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const entries = await readdir(this.directory);
    let removed = false;
    await Promise.all(entries.filter(name => /^[a-f0-9]{64}\.json$/.test(name)).map(async name => {
      const path = join(this.directory, name);
      try {
        const record = JSON.parse(await readFile(path, 'utf8')) as Partial<NonceRecord>;
        if (Number.isFinite(record.expiresAt) && Number(record.expiresAt) * 1000 <= nowMilliseconds) {
          await rm(path, { force: true });
          removed = true;
        }
      } catch {
        // Corrupt or incomplete evidence remains in place so exclusive creation
        // continues to reject the nonce. Operators must investigate and remove
        // it explicitly; cleanup must never turn corruption into replay access.
      }
    }));
    if (removed) await syncDirectory(this.directory);
    this.lastCleanup = nowMilliseconds;
  }
}

function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}

async function syncDirectory(directory: string): Promise<void> {
  const handle = await open(directory, 'r');
  try { await handle.sync(); } finally { await handle.close(); }
}
