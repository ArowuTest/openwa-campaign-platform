import { Injectable, Logger, OnModuleDestroy, OnModuleInit } from '@nestjs/common';
import { mkdir, readdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import type { ProviderMessageEvent } from './provider-event-publisher.service';

@Injectable()
export class ProviderEventOutboxService implements OnModuleInit, OnModuleDestroy {
  private readonly logger = new Logger(ProviderEventOutboxService.name);
  private readonly directory = String(process.env.GATEWAY_EVENT_OUTBOX_DIR ?? '/data/gateway-event-outbox');
  private readonly intervalMs = boundedInteger(process.env.GATEWAY_EVENT_OUTBOX_POLL_MS, 2_000, 250, 60_000);
  private timer?: NodeJS.Timeout;
  private sender?: (event: ProviderMessageEvent) => Promise<void>;
  private running = false;

  async onModuleInit() {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    this.timer = setInterval(() => void this.flush(), this.intervalMs);
    this.timer.unref();
  }
  async onModuleDestroy() {
    if (this.timer) clearInterval(this.timer);
    await this.flush();
  }
  setSender(sender: (event: ProviderMessageEvent) => Promise<void>) { this.sender = sender; }

  async enqueue(event: ProviderMessageEvent): Promise<void> {
    const name = safeName(event.eventId);
    const finalPath = join(this.directory, `${name}.json`);
    const temporaryPath = join(this.directory, `.${name}.${process.pid}.${Date.now()}.tmp`);
    const data = JSON.stringify(event);
    try {
      await writeFile(temporaryPath, data, { encoding: 'utf8', mode: 0o600, flag: 'wx' });
      await rename(temporaryPath, finalPath);
    } catch (error) {
      await rm(temporaryPath, { force: true }).catch(() => undefined);
      if ((error as NodeJS.ErrnoException)?.code !== 'EEXIST') throw error;
      const existing = await readFile(finalPath, 'utf8');
      if (existing !== data) throw new Error('gateway event outbox identifier was reused with different evidence');
    }
  }

  async acknowledge(eventId: string): Promise<void> {
    await rm(join(this.directory, `${safeName(eventId)}.json`), { force: true });
  }

  async flush(): Promise<void> {
    if (this.running || !this.sender) return;
    this.running = true;
    try {
      const names = (await readdir(this.directory)).filter(name => name.endsWith('.json')).sort().slice(0, 100);
      for (const name of names) {
        try {
          const event = JSON.parse(await readFile(join(this.directory, name), 'utf8')) as ProviderMessageEvent;
          await this.sender(event);
          await this.acknowledge(event.eventId);
        } catch (error) {
          this.logger.warn(`gateway event outbox delivery deferred: ${safeError(error)}`);
          break;
        }
      }
    } finally {
      this.running = false;
    }
  }
}

function safeName(value: string): string {
  if (!/^[A-Za-z0-9._:-]{1,200}$/.test(value)) throw new TypeError('gateway event identifier is unsafe for outbox storage');
  return Buffer.from(value).toString('base64url');
}
function safeError(value: unknown): string {
  const message = value instanceof Error ? value.message : String(value ?? 'unknown error');
  return message.replace(/[\r\n\t]+/g, ' ').slice(0, 300);
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
