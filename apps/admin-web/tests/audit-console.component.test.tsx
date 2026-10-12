import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { AuditConsole } from '../app/audit/audit-console';

const auth = vi.hoisted(() => ({
  session: { id: 'audit-user', sessionId: 'audit-session', permissions: ['audit.read'] }
}));

vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function event(sequence: number, action: string) {
  return {
    id: 'audit-' + sequence,
    sequence,
    actorType: 'USER',
    actorId: 'actor-' + sequence,
    action,
    objectType: 'CAMPAIGN',
    objectId: 'campaign-' + sequence,
    outcome: 'SUCCESS',
    sensitivity: 'HIGH',
    correlationId: 'corr-' + sequence,
    occurredAt: '2026-10-12T00:00:0' + sequence + 'Z',
    previousHash: sequence > 1 ? 'prev-' + sequence : '',
    hash: 'hash-' + sequence
  };
}

test('loads immutable audit events and requests the next sequence cursor', async () => {
  const calls: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    calls.push(path);
    if (path.includes('afterSequence=1')) {
      return new Response(JSON.stringify({ items: [event(2, 'CAMPAIGN_APPROVED')], count: 1, hasMore: false }), {
        status: 200,
        headers: { 'content-type': 'application/json' }
      });
    }
    return new Response(JSON.stringify({ items: [event(1, 'CAMPAIGN_CREATED')], count: 1, hasMore: true, nextCursor: 1 }), {
      status: 200,
      headers: { 'content-type': 'application/json' }
    });
  });

  render(<AuditConsole />);
  expect(await screen.findByRole('button', { name: /CAMPAIGN_CREATED/ })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Load more audit events' }));
  expect(await screen.findByRole('button', { name: /CAMPAIGN_APPROVED/ })).toBeTruthy();
  await waitFor(() => expect(calls.some((path) => path.includes('afterSequence=1'))).toBe(true));
});
