import { Injectable, ServiceUnavailableException } from '@nestjs/common';

type Waiter = { resolve: () => void; reject: (error: Error) => void };
type SessionState = {
  active: number;
  draining: boolean;
  waiters: Waiter[];
  nextAllowedAt: number;
  currentMessagesPerMinute: number;
  consecutiveSuccesses: number;
  idleWaiters: Array<() => void>;
  teardownPending: boolean;
  startPending: boolean;
  tornDown: boolean;
  rateTail: Promise<void>;
};

@Injectable()
export class SessionPipelineService {
  private readonly states = new Map<string, SessionState>();
  private readonly inFlightLimit = positiveInteger(process.env.SESSION_IN_FLIGHT_LIMIT, 1, 100);
  private readonly queueLimit = positiveInteger(process.env.SESSION_QUEUE_LIMIT, 1_000, 100_000);
  private readonly maximumStates = positiveInteger(process.env.SESSION_PIPELINE_MAX_STATES, 10_000, 1_000_000);
  private readonly configuredMessagesPerMinute = positiveInteger(process.env.SESSION_MESSAGES_PER_MINUTE, 20, 100_000);
  private readonly minimumMessagesPerMinute = positiveInteger(process.env.SESSION_MIN_MESSAGES_PER_MINUTE, 1, this.configuredMessagesPerMinute);

  async run<T>(sessionId: string, operation: () => Promise<T>): Promise<T> {
    const state = this.state(sessionId);
    if (state.draining || state.startPending || state.teardownPending || state.tornDown) throw new ServiceUnavailableException('session is not ready for governed submission');
    await this.acquire(state);
    try {
      if (state.draining || state.startPending || state.teardownPending || state.tornDown) throw new ServiceUnavailableException('session is not ready for governed submission');
      await this.waitForRateWindow(state);
      if (state.draining || state.startPending || state.teardownPending || state.tornDown) throw new ServiceUnavailableException('session is not ready for governed submission');
      return await operation();
    } finally {
      this.release(state);
    }
  }

  drain(sessionId: string) {
    const state = this.state(sessionId);
    if (state.startPending) throw new ServiceUnavailableException('session start is pending');
    this.enterDrain(state);
  }
  async drainForTeardown(sessionId: string): Promise<void> {
    const state = this.state(sessionId);
    if (state.startPending) throw new ServiceUnavailableException('session start is pending');
    if (state.teardownPending) throw new ServiceUnavailableException('session teardown is already pending');
    state.teardownPending = true;
    state.tornDown = true;
    this.enterDrain(state);
    if (state.active === 0) return;
    await new Promise<void>(resolve => state.idleWaiters.push(resolve));
  }
  finishTeardown(sessionId: string) { this.state(sessionId).teardownPending = false; }
  beginStart(sessionId: string) {
    const state = this.state(sessionId);
    if (state.teardownPending) throw new ServiceUnavailableException('session teardown is pending');
    if (state.startPending) throw new ServiceUnavailableException('session start is already pending');
    if (state.active > 0) throw new ServiceUnavailableException('session has active sends');
    state.startPending = true;
  }
  finishStart(sessionId: string, successful: boolean) {
    const state = this.state(sessionId);
    if (!state.startPending) return;
    state.startPending = false;
    if (successful) { state.draining = false; state.tornDown = false; }
    else state.tornDown = true;
  }
  resume(sessionId: string) {
    const state = this.state(sessionId);
    if (state.teardownPending) throw new ServiceUnavailableException('session teardown is pending');
    if (state.startPending) throw new ServiceUnavailableException('session start is pending');
    if (state.tornDown) throw new ServiceUnavailableException('session requires a successful governed start');
    state.draining = false;
  }
  status(sessionId: string) {
    const state = this.states.get(sessionId);
    if (!state) return { active: 0, queued: 0, draining: false, starting: false, tornDown: false, inFlightLimit: this.inFlightLimit, configuredMessagesPerMinute: this.configuredMessagesPerMinute, currentMessagesPerMinute: this.configuredMessagesPerMinute, nextAllowedAt: null };
    return {
      active: state.active,
      queued: state.waiters.length,
      draining: state.draining,
      starting: state.startPending,
      tornDown: state.tornDown,
      inFlightLimit: this.inFlightLimit,
      configuredMessagesPerMinute: this.configuredMessagesPerMinute,
      currentMessagesPerMinute: state.currentMessagesPerMinute,
      nextAllowedAt: state.nextAllowedAt ? new Date(state.nextAllowedAt).toISOString() : null
    };
  }

  recordProviderSuccess(sessionId: string) {
    const state = this.states.get(sessionId);
    if (state) this.recordSuccess(state);
  }
  recordProviderFailure(sessionId: string) {
    const state = this.states.get(sessionId);
    if (state) this.recordFailure(state);
  }
  forgetTornDown(sessionId: string): boolean {
    const state = this.states.get(sessionId);
    if (!state || !state.tornDown || state.active !== 0 || state.waiters.length !== 0 || state.startPending || state.teardownPending) return false;
    return this.states.delete(sessionId);
  }

  private state(sessionId: string): SessionState {
    let state = this.states.get(sessionId);
    if (!state) {
      if (this.states.size >= this.maximumStates) throw new ServiceUnavailableException('session pipeline state capacity is exhausted');
      state = {
        active: 0, draining: false, waiters: [], nextAllowedAt: 0,
        currentMessagesPerMinute: this.configuredMessagesPerMinute,
        consecutiveSuccesses: 0, idleWaiters: [], teardownPending: false, startPending: false, tornDown: false, rateTail: Promise.resolve()
      };
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
    if (state.active === 0) {
      for (const resolve of state.idleWaiters.splice(0)) resolve();
    }
    if (state.draining) return;
    const next = state.waiters.shift();
    if (next) next.resolve();
  }
  private enterDrain(state: SessionState) {
    state.draining = true;
    const waiters = state.waiters.splice(0);
    const error = new ServiceUnavailableException('session is draining');
    for (const waiter of waiters) waiter.reject(error);
  }
  private async waitForRateWindow(state: SessionState) {
    const previous = state.rateTail;
    let release!: () => void;
    state.rateTail = new Promise<void>(resolve => { release = resolve; });
    await previous;
    try {
      const now = Date.now();
      const wait = Math.max(0, state.nextAllowedAt - now);
      if (wait > 0) await new Promise(resolve => setTimeout(resolve, wait));
      const interval = Math.ceil(60_000 / Math.max(this.minimumMessagesPerMinute, state.currentMessagesPerMinute));
      state.nextAllowedAt = Date.now() + interval;
    } finally {
      release();
    }
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
