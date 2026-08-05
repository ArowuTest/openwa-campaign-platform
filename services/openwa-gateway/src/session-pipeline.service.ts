import { Injectable, ServiceUnavailableException } from '@nestjs/common';

type Waiter = { resolve: () => void; reject: (error: Error) => void };
type SessionState = {
  active: number;
  draining: boolean;
  waiters: Waiter[];
  nextAllowedAt: number;
  currentMessagesPerMinute: number;
  consecutiveSuccesses: number;
};

@Injectable()
export class SessionPipelineService {
  private readonly states = new Map<string, SessionState>();
  private readonly inFlightLimit = positiveInteger(process.env.SESSION_IN_FLIGHT_LIMIT, 1, 100);
  private readonly queueLimit = positiveInteger(process.env.SESSION_QUEUE_LIMIT, 1_000, 100_000);
  private readonly configuredMessagesPerMinute = positiveInteger(process.env.SESSION_MESSAGES_PER_MINUTE, 20, 100_000);
  private readonly minimumMessagesPerMinute = positiveInteger(process.env.SESSION_MIN_MESSAGES_PER_MINUTE, 1, this.configuredMessagesPerMinute);

  async run<T>(sessionId: string, operation: () => Promise<T>): Promise<T> {
    const state = this.state(sessionId);
    if (state.draining) throw new ServiceUnavailableException('session is draining');
    await this.acquire(state);
    try {
      if (state.draining) throw new ServiceUnavailableException('session entered drain before execution');
      await this.waitForRateWindow(state);
      try {
        const result = await operation();
        this.recordSuccess(state);
        return result;
      } catch (error) {
        this.recordFailure(state);
        throw error;
      }
    } finally {
      this.release(state);
    }
  }

  drain(sessionId: string) {
    const state = this.state(sessionId);
    state.draining = true;
    const waiters = state.waiters.splice(0);
    const error = new ServiceUnavailableException('session is draining');
    for (const waiter of waiters) waiter.reject(error);
  }
  resume(sessionId: string) { this.state(sessionId).draining = false; }
  status(sessionId: string) {
    const state = this.state(sessionId);
    return {
      active: state.active,
      queued: state.waiters.length,
      draining: state.draining,
      inFlightLimit: this.inFlightLimit,
      configuredMessagesPerMinute: this.configuredMessagesPerMinute,
      currentMessagesPerMinute: state.currentMessagesPerMinute,
      nextAllowedAt: state.nextAllowedAt ? new Date(state.nextAllowedAt).toISOString() : null
    };
  }

  private state(sessionId: string): SessionState {
    let state = this.states.get(sessionId);
    if (!state) {
      state = { active: 0, draining: false, waiters: [], nextAllowedAt: 0, currentMessagesPerMinute: this.configuredMessagesPerMinute, consecutiveSuccesses: 0 };
      this.states.set(sessionId, state);
    }
    return state;
  }
  private acquire(state: SessionState): Promise<void> {
    if (state.active < this.inFlightLimit) { state.active += 1; return Promise.resolve(); }
    if (state.waiters.length >= this.queueLimit) throw new ServiceUnavailableException('session queue is full');
    return new Promise<void>((resolve, reject) => state.waiters.push({ resolve: () => { state.active += 1; resolve(); }, reject }));
  }
  private release(state: SessionState) {
    state.active = Math.max(0, state.active - 1);
    if (state.draining) return;
    const next = state.waiters.shift();
    if (next) next.resolve();
  }
  private async waitForRateWindow(state: SessionState) {
    const now = Date.now();
    const wait = Math.max(0, state.nextAllowedAt - now);
    if (wait > 0) await new Promise(resolve => setTimeout(resolve, wait));
    const interval = Math.ceil(60_000 / Math.max(this.minimumMessagesPerMinute, state.currentMessagesPerMinute));
    state.nextAllowedAt = Math.max(Date.now(), state.nextAllowedAt) + interval;
  }
  private recordSuccess(state: SessionState) {
    state.consecutiveSuccesses += 1;
    if (state.consecutiveSuccesses >= 20 && state.currentMessagesPerMinute < this.configuredMessagesPerMinute) {
      state.currentMessagesPerMinute = Math.min(this.configuredMessagesPerMinute, state.currentMessagesPerMinute + 1);
      state.consecutiveSuccesses = 0;
    }
  }
  private recordFailure(state: SessionState) {
    state.consecutiveSuccesses = 0;
    state.currentMessagesPerMinute = Math.max(this.minimumMessagesPerMinute, Math.floor(state.currentMessagesPerMinute / 2));
  }
}

function positiveInteger(value: string | undefined, fallback: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed > 0 && parsed <= maximum ? parsed : fallback;
}
