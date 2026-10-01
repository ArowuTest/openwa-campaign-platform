import { performance } from 'node:perf_hooks';

export type OwnershipRequest = { sequence: number; startedAt: number };
type OwnedSession = {
  firstVersion: number;
  latestVersion: number;
  deadline: number;
  status: string;
  timer: NodeJS.Timeout;
};

// Deliberately process-local: a durable dispatch fence is not proof that a new
// process owns a lease. Only an authenticated heartbeat response seeds this map.
export class SessionOwnershipRegistry {
  private readonly sessions = new Map<string, OwnedSession>();
  private readonly pending = new Map<string, OwnershipRequest>();
  private readonly listeners = new Set<(sessionId: string) => void>();
  private sequence = 0;

  begin(sessionId: string): OwnershipRequest {
    const request = { sequence: ++this.sequence, startedAt: performance.now() };
    this.pending.set(sessionId, request);
    return request;
  }

  accept(sessionId: string, nodeId: string, bootId: string, request: OwnershipRequest, response: unknown): void {
    if (this.pending.get(sessionId) !== request) throw new Error('session ownership response was superseded');
    const payload = response as { status?: unknown; ownership?: Record<string, unknown> } | null;
    const proof = payload?.ownership;
    if (!proof || typeof proof !== 'object' || Array.isArray(proof) ||
        proof.nodeId !== nodeId || proof.sessionId !== sessionId || proof.bootId !== bootId ||
        typeof proof.leaseVersion !== 'number' || !Number.isSafeInteger(proof.leaseVersion) || proof.leaseVersion < 1 ||
        typeof proof.leaseExpiresAt !== 'string' || typeof proof.serverNow !== 'string' || typeof payload?.status !== 'string') {
      throw new Error('control plane returned invalid boot-bound session ownership');
    }
    const duration = Date.parse(proof.leaseExpiresAt) - Date.parse(proof.serverNow);
    if (!Number.isFinite(duration) || duration <= 1_000 || duration > 600_000) throw new Error('control plane returned invalid session lease expiry');
    // Anchor to request START on a monotonic clock, not response receipt or local
    // wall time. Network/processing delay can shorten, never extend, authority.
    const deadline = request.startedAt + duration - 1_000;
    if (deadline <= performance.now()) throw new Error('session ownership response arrived after its usable lease');
    const previous = this.sessions.get(sessionId);
    if (previous && previous.deadline <= performance.now()) {
      this.revoke(sessionId);
      throw new Error('expired session ownership must retire before recovery');
    }
    if (previous && proof.leaseVersion <= previous.latestVersion) throw new Error('session ownership response did not advance the lease fence');
    if (!['PAIRING', 'CONNECTING', 'READY', 'BUSY', 'DRAINING', 'PAUSED', 'DISCONNECTED', 'RECOVERING', 'FAILED_RECOVERY', 'RESTRICTED', 'QUARANTINED', 'RETIRED'].includes(payload.status)) {
      throw new Error('control plane returned invalid session lifecycle state');
    }
    if (previous) clearTimeout(previous.timer);
    const timer = setTimeout(() => this.revoke(sessionId), Math.max(1, deadline - performance.now()));
    timer.unref();
    this.sessions.set(sessionId, {
      firstVersion: previous?.firstVersion ?? proof.leaseVersion,
      latestVersion: proof.leaseVersion,
      deadline, status: payload.status, timer,
    });
    this.pending.delete(sessionId);
  }

  assertOwned(sessionId: string, dispatchVersion?: number): void {
    const owned = this.sessions.get(sessionId);
    if (!owned || owned.deadline <= performance.now()) {
      this.revoke(sessionId);
      throw new Error('current gateway process does not own a live session lease');
    }
    if (['DRAINING', 'FAILED_RECOVERY', 'RESTRICTED', 'QUARANTINED', 'RETIRED'].includes(owned.status)) {
      throw new Error('session ownership is not activatable in its governed state');
    }
    if (dispatchVersion !== undefined && (
      !Number.isSafeInteger(dispatchVersion) || dispatchVersion < owned.firstVersion || dispatchVersion > owned.latestVersion ||
      !['READY', 'BUSY'].includes(owned.status))) {
      throw new Error('dispatch authority does not belong to the accepted current-boot lease');
    }
  }

  reject(sessionId: string, request: OwnershipRequest): void {
    if (this.pending.get(sessionId) === request) this.revoke(sessionId);
  }
  revoke(sessionId: string): void {
    const owned = this.sessions.get(sessionId);
    this.pending.delete(sessionId);
    if (!owned) return;
    clearTimeout(owned.timer);
    this.sessions.delete(sessionId);
    for (const listener of this.listeners) listener(sessionId);
  }
  revokeAll(): void {
    this.pending.clear();
    for (const sessionId of [...this.sessions.keys()]) this.revoke(sessionId);
  }
  onLoss(listener: (sessionId: string) => void): () => void {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  }
}
