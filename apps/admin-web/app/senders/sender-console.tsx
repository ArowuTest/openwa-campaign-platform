'use client';

import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';

import { StatusBadge } from '../../components/status-badge';
import { useAuth } from '../../components/auth-provider';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type SenderPool = {
  id: string;
  name: string;
  organisationId?: string;
  status: string;
  maxMessagesPerMinute: number;
  dailyCapacity: number;
  reservedCapacity: number;
  version: number;
};

type SenderSession = {
  id: string;
  nodeId?: string;
  poolId?: string;
  gatewayPoolId?: string;
  maskedMsisdn: string;
  ownerReference: string;
  profileDisplayName: string;
  engineType: string;
  engineVersion?: string;
  status: string;
  safeMessagesPerMinute: number;
  safeDailyCapacity: number;
  sentToday: number;
  lastHeartbeatAt?: string;
  quarantineReason?: string;
  version: number;
};

type SenderNode = {
  id: string;
  name: string;
  gatewayPoolId?: string;
  provider?: string;
  engine?: string;
  adapterVersion?: string;
  status: string;
  sessionCount: number;
  queueDepth: number;
  draining: boolean;
  lastHeartbeatAt?: string;
  version: number;
};

type TestRecipient = {
  id: string;
  label: string;
  maskedMsisdn: string;
  status: string;
  version: number;
  reason: string;
  updatedAt: string;
};

function isStepUp(error: unknown) {
  return error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP');
}

export function SenderConsole() {
  const router = useRouter();
  const { session } = useAuth();
  const [pools, setPools] = useState<SenderPool[]>([]);
  const [sessions, setSessions] = useState<SenderSession[]>([]);
  const [nodes, setNodes] = useState<SenderNode[]>([]);
  const [testRecipients, setTestRecipients] = useState<TestRecipient[]>([]);
  const [selectedSessionId, setSelectedSessionId] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');

  const canOperate = hasPermission(session?.permissions, 'sender.operate');
  const canWrite = hasPermission(session?.permissions, 'sender.write');
  const canApprove = hasPermission(session?.permissions, 'sender.approve');

  const fetchInventory = useCallback(async () => Promise.all([
    collectBoundedPages<SenderPool>((cursor) => apiRequest<ListEnvelope<SenderPool>>('/v1/sender-pools?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
    collectBoundedPages<SenderSession>((cursor) => apiRequest<ListEnvelope<SenderSession>>('/v1/sender-sessions?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
    collectBoundedPages<SenderNode>((cursor) => apiRequest<ListEnvelope<SenderNode>>('/v1/sender-nodes?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
    collectBoundedPages<TestRecipient>((cursor) => apiRequest<ListEnvelope<TestRecipient>>('/v1/admin/test-recipients?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20)
  ]), []);

  const applyInventory = useCallback((values: Awaited<ReturnType<typeof fetchInventory>>) => {
    const [poolItems, sessionItems, nodeItems, recipientItems] = values;
    setPools(poolItems);
    setSessions(sessionItems);
    setNodes(nodeItems);
    setTestRecipients(recipientItems);
  }, []);

  const load = useCallback(async () => {
    applyInventory(await fetchInventory());
  }, [applyInventory, fetchInventory]);

  useEffect(() => {
    let cancelled = false;
    void fetchInventory()
      .then((values) => {
        if (!cancelled) applyInventory(values);
      })
      .catch((cause) => {
        if (!cancelled) setMessage(cause instanceof Error ? cause.message : 'Sender inventory could not be loaded.');
      });
    return () => { cancelled = true; };
  }, [applyInventory, fetchInventory]);

  const selected = useMemo(() => sessions.find((item) => item.id === selectedSessionId), [selectedSessionId, sessions]);

  async function mutate(label: string, work: () => Promise<unknown>) {
    if (busy) return;
    setBusy(label);
    setMessage('');
    try {
      await work();
      setMessage(label + ' completed. Inventory refreshed.');
      await load();
    } catch (cause) {
      if (isStepUp(cause)) {
        router.push('/step-up?returnTo=' + encodeURIComponent('/senders'));
      } else {
        setMessage(cause instanceof Error ? cause.message : label + ' failed.');
      }
    } finally {
      setBusy('');
    }
  }

  async function lifecycle(action: 'start' | 'drain' | 'resume' | 'stop') {
    if (!selected || reason.trim().length < 3) {
      setMessage('Select a session and enter an operational reason.');
      return;
    }
    await mutate(action + ' session', () => apiRequest('/v1/sender-sessions/' + selected.id + '/' + action, {
      method: 'POST',
      body: JSON.stringify({ expectedVersion: selected.version, reason: reason.trim() })
    }));
  }

  async function createTestRecipient(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    await mutate('Create test recipient', () => apiRequest('/v1/admin/test-recipients', {
      method: 'POST',
      body: JSON.stringify({
        label: String(data.get('label') ?? '').trim(),
        msisdn: String(data.get('msisdn') ?? '').trim(),
        reason: String(data.get('reason') ?? '').trim()
      })
    }));
    event.currentTarget.reset();
  }

  async function testRecipientAction(item: TestRecipient, action: 'submit' | 'approve' | 'reject' | 'revoke') {
    const path = action === 'submit'
      ? '/v1/admin/test-recipients/' + item.id + '/submit'
      : action === 'revoke'
        ? '/v1/admin/test-recipients/' + item.id + '/revoke'
        : '/v1/admin/test-recipients/' + item.id + '/decision';
    await mutate(action + ' test recipient', () => apiRequest(path, {
      method: 'POST',
      body: JSON.stringify({
        expectedVersion: item.version,
        reason: action === 'submit' ? 'Submitted for independent test-recipient approval' : 'Reviewed in governed sender console',
        approve: action === 'approve'
      })
    }));
  }

  const readySessions = sessions.filter((item) => item.status === 'READY').length;
  const unhealthyNodes = nodes.filter((item) => item.status !== 'READY' || item.draining).length;

  return (
    <div className="workspace-stack">
      {message ? <div className={message.includes('failed') || message.includes('Select') ? 'alert alert-danger' : 'alert alert-information'} role="status">{message}</div> : null}

      <section className="grid grid-4" aria-label="Sender inventory summary">
        <article className="card metric"><span className="muted">Active pools</span><strong>{pools.filter((item) => item.status === 'ACTIVE').length}</strong></article>
        <article className="card metric"><span className="muted">READY sessions</span><strong>{readySessions}</strong></article>
        <article className="card metric"><span className="muted">Nodes needing attention</span><strong>{unhealthyNodes}</strong></article>
        <article className="card metric"><span className="muted">Approved test recipients</span><strong>{testRecipients.filter((item) => item.status === 'ACTIVE').length}</strong></article>
      </section>

      <section className="card">
        <div className="section-heading"><div><h2>Sender sessions</h2><p className="muted">Only masked MSISDNs are shown. Session authority remains server-side and lease-fenced.</p></div></div>
        {sessions.length === 0 ? <div className="empty-state"><strong>No sessions</strong><p>Register and pair a governed OpenWA session before campaign UAT.</p></div> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Sender</th><th>Engine</th><th>Status</th><th>Pool / node</th><th>Capacity</th><th>Heartbeat</th></tr></thead>
              <tbody>{sessions.map((item) => (
                <tr key={item.id} className={selectedSessionId === item.id ? 'selected-row' : undefined} onClick={() => setSelectedSessionId(item.id)}>
                  <td><strong>{item.profileDisplayName || item.ownerReference}</strong><small>{item.maskedMsisdn}</small></td>
                  <td>{item.engineType}<small>{item.engineVersion || 'Version unavailable'}</small></td>
                  <td><StatusBadge status={item.status} />{item.quarantineReason ? <small>{item.quarantineReason}</small> : null}</td>
                  <td>{item.poolId || '—'}<small>Node: {item.nodeId || '—'}</small></td>
                  <td>{item.safeMessagesPerMinute}/min<small>{item.safeDailyCapacity.toLocaleString()}/day · {item.sentToday.toLocaleString()} sent today</small></td>
                  <td>{item.lastHeartbeatAt ? new Date(item.lastHeartbeatAt).toLocaleString() : 'No heartbeat'}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>

      <section className="grid grid-2">
        <article className="card form-stack">
          <h2>Session lifecycle</h2>
          <p className="muted">{selected ? 'Selected: ' + (selected.profileDisplayName || selected.maskedMsisdn) + ' · version ' + selected.version : 'Select a session from the table above.'}</p>
          <label>Operational reason<textarea rows={3} value={reason} onChange={(event) => setReason(event.target.value)} /></label>
          <div className="button-row">
            <button className="primary" type="button" disabled={!canOperate || !selected || Boolean(busy)} onClick={() => void lifecycle('start')}>Start</button>
            <button className="secondary" type="button" disabled={!canOperate || !selected || Boolean(busy)} onClick={() => void lifecycle('drain')}>Drain</button>
            <button className="secondary" type="button" disabled={!canOperate || !selected || Boolean(busy)} onClick={() => void lifecycle('resume')}>Resume</button>
            <button className="danger-ghost" type="button" disabled={!canOperate || !selected || Boolean(busy)} onClick={() => void lifecycle('stop')}>Stop</button>
          </div>
          <small>Pairing credentials and QR payloads are intentionally excluded from this inventory surface.</small>
        </article>

        <article className="card">
          <h2>Gateway nodes</h2>
          {nodes.length === 0 ? <div className="empty-state"><strong>No nodes</strong><p>No governed gateway runtime has registered.</p></div> : (
            <div className="table-wrap"><table><thead><tr><th>Node</th><th>Route</th><th>Status</th><th>Load</th></tr></thead><tbody>{nodes.map((node) => <tr key={node.id}><td><strong>{node.name}</strong><small>{node.adapterVersion || 'Adapter unknown'}</small></td><td>{node.provider || '—'} / {node.engine || '—'}</td><td><StatusBadge status={node.status} />{node.draining ? <small>Draining</small> : null}</td><td>{node.sessionCount} sessions<small>Queue {node.queueDepth}</small></td></tr>)}</tbody></table></div>
          )}
        </article>
      </section>

      <section className="card">
        <h2>Sender pools</h2>
        {pools.length === 0 ? <div className="empty-state"><strong>No sender pools</strong><p>Create governed capacity before campaign routing.</p></div> : (
          <div className="table-wrap"><table><thead><tr><th>Pool</th><th>Status</th><th>MPM</th><th>Daily capacity</th><th>Reserved</th></tr></thead><tbody>{pools.map((pool) => <tr key={pool.id}><td><strong>{pool.name}</strong><small>{pool.id}</small></td><td><StatusBadge status={pool.status} /></td><td>{pool.maxMessagesPerMinute}</td><td>{pool.dailyCapacity.toLocaleString()}</td><td>{pool.reservedCapacity.toLocaleString()}</td></tr>)}</tbody></table></div>
        )}
      </section>

      <section className="grid grid-2">
        <article className="card">
          <h2>Approved test recipients</h2>
          {testRecipients.length === 0 ? <div className="empty-state"><strong>No test recipients</strong><p>Create a test number, submit it, then obtain independent approval.</p></div> : (
            <div className="table-wrap"><table><thead><tr><th>Recipient</th><th>Status</th><th>Governance</th></tr></thead><tbody>{testRecipients.map((item) => <tr key={item.id}><td><strong>{item.label}</strong><small>{item.maskedMsisdn}</small></td><td><StatusBadge status={item.status} /></td><td><div className="button-row">{item.status === 'DRAFT' && canWrite ? <button className="secondary compact-button" type="button" onClick={() => void testRecipientAction(item, 'submit')}>Submit</button> : null}{item.status === 'PENDING_APPROVAL' && canApprove ? <><button className="primary compact-button" type="button" onClick={() => void testRecipientAction(item, 'approve')}>Approve</button><button className="danger-ghost compact-button" type="button" onClick={() => void testRecipientAction(item, 'reject')}>Reject</button></> : null}{item.status === 'ACTIVE' && canApprove ? <button className="danger-ghost compact-button" type="button" onClick={() => void testRecipientAction(item, 'revoke')}>Revoke</button> : null}</div></td></tr>)}</tbody></table></div>
          )}
        </article>

        <form className="card form-stack" onSubmit={createTestRecipient}>
          <h2>Add test recipient</h2>
          <p className="muted">The server encrypts the E.164 number and returns only a masked value to normal inventory views.</p>
          <label>Label<input name="label" required /></label>
          <label>E.164 test number<input name="msisdn" type="tel" placeholder="+234…" autoComplete="off" required /></label>
          <label>Reason<textarea name="reason" minLength={8} rows={3} required /></label>
          <button className="primary" type="submit" disabled={!canWrite || Boolean(busy)}>Create draft test recipient</button>
        </form>
      </section>
    </div>
  );
}
