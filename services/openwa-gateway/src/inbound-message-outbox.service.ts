import { Injectable, Logger, OnModuleDestroy, OnModuleInit } from '@nestjs/common';
import { randomUUID } from 'node:crypto';
import { link, mkdir, open, readdir, readFile, rename, rm } from 'node:fs/promises';
import { join } from 'node:path';
import type { InboundMessageEvent } from './inbound-message-publisher.service';

@Injectable()
export class InboundMessageOutboxService implements OnModuleInit, OnModuleDestroy {
  private readonly logger = new Logger(InboundMessageOutboxService.name);
  private readonly directory = String(process.env.GATEWAY_INBOUND_OUTBOX_DIR ?? '/data/gateway-inbound-outbox');
  private readonly quarantineDirectory = join(this.directory, '.quarantine');
  private readonly intervalMs = boundedInteger(process.env.GATEWAY_INBOUND_OUTBOX_POLL_MS, 2_000, 250, 60_000);
  private timer?: NodeJS.Timeout;
  private sender?: (event: InboundMessageEvent) => Promise<void>;
  private running = false;

  async onModuleInit() {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    await mkdir(this.quarantineDirectory, { recursive: true, mode: 0o700 });
    this.timer = setInterval(() => void this.flush(), this.intervalMs);
    this.timer.unref();
  }
  async onModuleDestroy() {
    if (this.timer) clearInterval(this.timer);
    await this.flush();
  }
  setSender(sender: (event: InboundMessageEvent) => Promise<void>) { this.sender = sender; }

  async enqueue(event: InboundMessageEvent): Promise<void> {
    const name = safeName(event.eventId);
    const finalPath = join(this.directory, `${name}.json`);
    const data = JSON.stringify(event);
    try {
      await writeDurableExclusive(finalPath, data, this.directory);
    } catch (error) {
      if ((error as NodeJS.ErrnoException)?.code !== 'EEXIST') throw error;
      const existing = await readFile(finalPath, 'utf8');
      if (existing !== data) throw new Error('inbound event identifier was reused with different evidence');
    }
  }

  async acknowledge(eventId: string): Promise<void> {
    await removeDurable(join(this.directory, `${safeName(eventId)}.json`), this.directory);
  }

  async flush(): Promise<void> {
    if (this.running || !this.sender) return;
    this.running = true;
    try {
      const names = (await readdir(this.directory)).filter(name => name.endsWith('.json')).sort().slice(0, 100);
      for (const name of names) {
        const filePath = join(this.directory, name);
        let event: InboundMessageEvent;
        try {
          event = JSON.parse(await readFile(filePath, 'utf8')) as InboundMessageEvent;
          if (!event || typeof event.eventId !== 'string' || `${safeName(event.eventId)}.json` !== name) {
            throw new Error('inbound event outbox filename does not match payload identity');
          }
        } catch (error) {          try {
            await this.quarantine(name, error);
          } catch (quarantineError) {
            this.logger.warn('inbound event quarantine failed', { eventFile: name, error: safeError(quarantineError) });
            break;
          }
          continue;
        }
        try {
          await this.sender(event);
          await removeDurable(filePath, this.directory);
        } catch (error) {
          this.logger.warn('inbound event delivery remains pending', { eventFile: name, error: safeError(error) });
          break;
        }
      }
    } finally { this.running = false; }
  }

  private async quarantine(name: string, error: unknown): Promise<void> {
    const source = join(this.directory, name);
    const target = join(this.quarantineDirectory, `${name}.${Date.now()}-${randomUUID()}.quarantine`);
    await rename(source, target);
    await syncDirectory(this.directory);
    await syncDirectory(this.quarantineDirectory);
    this.logger.warn('inbound event outbox quarantined invalid evidence', { eventFile: name, error: safeError(error) });
  }
}
async function writeDurableExclusive(finalPath: string, data: string, directory: string): Promise<void> {
  const tempPath = join(directory, `.pending-${randomUUID()}.tmp`);
  try {
    const handle = await open(tempPath, 'wx', 0o600);
    try {
      await handle.writeFile(data, { encoding: 'utf8' });
      await handle.sync();
    } finally {
      await handle.close();
    }
    await link(tempPath, finalPath);
    await syncDirectory(directory);
  } finally {
    await rm(tempPath, { force: true });
    await syncDirectory(directory);
  }
}

async function removeDurable(path: string, directory: string): Promise<void> {
  await rm(path, { force: true });
  await syncDirectory(directory);
}

async function syncDirectory(directory: string): Promise<void> {
  const handle = await open(directory, 'r');
  try { await handle.sync(); } finally { await handle.close(); }
}
function safeName(value: string): string {
  if (!/^[A-Za-z0-9._:-]{1,200}$/.test(value)) throw new TypeError('inbound event identifier is invalid');
  return Buffer.from(value).toString('base64url');
}
function safeError(value: unknown): string {
  return (value instanceof Error ? value.message : String(value ?? '')).replace(/[\r\n\t]+/g, ' ').slice(0, 300);
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
