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

type PoolCapacity = {
  poolId: string;
  readySessions: number;
  healthyNodes: number;
  configuredMessagesPerMinute: number;
  availableMessagesPerMinute: number;
  configuredDailyCapacity: number;
  remainingDailyCapacity: number;
  reservedCapacity: number;
  availableDailyCapacity: number;
  asAt: string;
};

type SenderSession = {
  id: string;
  nodeId?: string;
  poolId?: string;
  gatewayPoolId?: string;
  maskedMsisdn: string;
  ownerReference: string;
  registrationCountryIso2: string;
  profileDisplayName: string;
  recoveryReferenceConfigured?: boolean;
  engineType: string;
  engineVersion?: string;
  stateVolumeReference?: string;
  status: string;
  safeMessagesPerMinute: number;
  safeDailyCapacity: number;
  inFlightLimit?: number;
  sentToday: number;
  lastHeartbeatAt?: string;
  quarantineReason?: string;
  version: number;
};

type SenderGatewayResult = {
  sessionId: string;
  status?: string;
};

type SenderHealth = SenderGatewayResult;

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

function isFailureMessage(value: string) {
  return value.includes('failed') || value.includes('Select') || value.includes('required');
}

export function SenderConsole() {
  const router = useRouter();
  const { session } = useAuth();
  const [pools, setPools] = useState<SenderPool[]>([]);
  const [sessions, setSessions] = useState<SenderSession[]>([]);
  const [nodes, setNodes] = useState<SenderNode[]>([]);
  const [testRecipients, setTestRecipients] = useState<TestRecipient[]>([]);
  const [selectedPoolId, setSelectedPoolId] = useState('');
  const [selectedSessionId, setSelectedSessionId] = useState('');
  const [capacity, setCapacity] = useState<PoolCapacity>();
  const [health, setHealth] = useState<SenderHealth>();
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [transientPairing, setTransientPairing] = useState('');

  const canOperate = hasPermission(session?.permissions, 'sender.operate');
  const canAdmin = hasPermission(session?.permissions, 'sender.admin');
  const canRead = hasPermission(session?.permissions, 'sender.read');
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

  const selectedPool = useMemo(() => pools.find((item) => item.id === selectedPoolId), [pools, selectedPoolId]);
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
    if (!selected || reason.trim().length < 8) {
      setMessage('Select a session and enter an operational reason of at least 8 characters.');
      return;
    }
    await mutate(action + ' session', () => apiRequest('/v1/sender-sessions/' + selected.id + '/' + action, {
      method: 'POST',
      body: JSON.stringify({ expectedVersion: selected.version, reason: reason.trim() })
    }));
  }

  async function createPool(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    await mutate('Create sender pool', () => apiRequest('/v1/sender-pools', {
      method: 'POST',
      body: JSON.stringify({
        name: String(data.get('name') ?? '').trim(),
        organisationId: String(data.get('organisationId') ?? '').trim(),
        status: String(data.get('status') ?? 'ACTIVE'),
        maxMessagesPerMinute: Number(data.get('maxMessagesPerMinute') ?? 0),
        dailyCapacity: Number(data.get('dailyCapacity') ?? 0),
        reservedCapacity: Number(data.get('reservedCapacity') ?? 0),
        reason: String(data.get('reason') ?? '').trim()
      })
    }));
    form.reset();
  }

  async function updatePool(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedPool) {
      setMessage('Select a sender pool before editing it.');
      return;
    }
    const data = new FormData(event.currentTarget);
    await mutate('Update sender pool', () => apiRequest('/v1/sender-pools/' + selectedPool.id, {
      method: 'PUT',
      body: JSON.stringify({
        name: String(data.get('name') ?? '').trim(),
        status: String(data.get('status') ?? 'ACTIVE'),
        maxMessagesPerMinute: Number(data.get('maxMessagesPerMinute') ?? 0),
        dailyCapacity: Number(data.get('dailyCapacity') ?? 0),
        reservedCapacity: Number(data.get('reservedCapacity') ?? 0),
        expectedVersion: selectedPool.version,
        reason: String(data.get('reason') ?? '').trim()
      })
    }));
  }

  async function readCapacity(pool: SenderPool) {
    setSelectedPoolId(pool.id);
    await mutate('Read pool capacity', async () => {
      const value = await apiRequest<PoolCapacity>('/v1/sender-pools/' + pool.id + '/capacity');
      setCapacity(value);
    });
  }

  async function registerSession(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    await mutate('Register sender session', () => apiRequest('/v1/sender-sessions', {
      method: 'POST',
      body: JSON.stringify({
        nodeId: String(data.get('nodeId') ?? '').trim(),
        poolId: String(data.get('poolId') ?? '').trim(),
        gatewayPoolId: String(data.get('gatewayPoolId') ?? '').trim(),
        stateVolumeReference: String(data.get('stateVolumeReference') ?? '').trim(),
        msisdn: String(data.get('msisdn') ?? '').trim(),
        ownerReference: String(data.get('ownerReference') ?? '').trim(),
        registrationCountryIso2: String(data.get('registrationCountryIso2') ?? '').trim().toUpperCase(),
        profileDisplayName: String(data.get('profileDisplayName') ?? '').trim(),
        recoveryReference: String(data.get('recoveryReference') ?? '').trim(),
        engineType: String(data.get('engineType') ?? 'BAILEYS'),
        engineVersion: String(data.get('engineVersion') ?? '').trim(),
        status: 'NEW',
        safeMessagesPerMinute: Number(data.get('safeMessagesPerMinute') ?? 0),
        safeDailyCapacity: Number(data.get('safeDailyCapacity') ?? 0),
        inFlightLimit: Number(data.get('inFlightLimit') ?? 0),
        reason: String(data.get('reason') ?? '').trim()
      })
    }));
    form.reset();
  }

  async function updateMetadata(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected) {
      setMessage('Select a sender session before editing metadata.');
      return;
    }
    const data = new FormData(event.currentTarget);
    await mutate('Update sender metadata', () => apiRequest('/v1/sender-sessions/' + selected.id + '/metadata', {
      method: 'PUT',
      body: JSON.stringify({
        ownerReference: String(data.get('ownerReference') ?? '').trim(),
        registrationCountryIso2: String(data.get('registrationCountryIso2') ?? '').trim().toUpperCase(),
        profileDisplayName: String(data.get('profileDisplayName') ?? '').trim(),
        recoveryReference: String(data.get('recoveryReference') ?? '').trim(),
        expectedVersion: selected.version,
        reason: String(data.get('reason') ?? '').trim()
      })
    }));
  }

  async function readHealth() {
    if (!selected) {
      setMessage('Select a sender session before reading health.');
      return;
    }
    await mutate('Read sender health', async () => {
      const value = await apiRequest<SenderHealth>('/v1/sender-sessions/' + selected.id + '/health');
      setHealth({ sessionId: value.sessionId, status: value.status });
    });
  }

  async function requestPairing(kind: 'qr' | 'code', event?: FormEvent<HTMLFormElement>) {
    if (!selected) {
      setMessage('Select a sender session before requesting pairing.');
      return;
    }
    if (kind === 'qr') {
      await mutate('Request pairing QR', async () => {
        await apiRequest<SenderGatewayResult>('/v1/sender-sessions/' + selected.id + '/pairing/qr');
        setTransientPairing('QR pairing response received. It is available only for this interaction and is never stored by the console.');
      });
      return;
    }
    event?.preventDefault();
    const pairingForm = event?.currentTarget;
    const data = pairingForm ? new FormData(pairingForm) : undefined;
    const phoneNumber = String(data?.get('phoneNumber') ?? '').trim();
    const pairingReason = String(data?.get('reason') ?? '').trim();
    if (phoneNumber.length < 8 || pairingReason.length < 8) {
      setMessage('Enter a pairing phone number and a reason of at least 8 characters.');
      return;
    }
    await mutate('Request pairing code', async () => {
      await apiRequest<SenderGatewayResult>('/v1/sender-sessions/' + selected.id + '/pairing/code', {
        method: 'POST',
        body: JSON.stringify({ expectedVersion: selected.version, phoneNumber, reason: pairingReason })
      });
      setTransientPairing('Pairing code response received. It is available only for this interaction and is never stored by the console.');
      pairingForm?.reset();
    });
  }

  async function createTestRecipient(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    await mutate('Create test recipient', () => apiRequest('/v1/admin/test-recipients', {
      method: 'POST',
      body: JSON.stringify({
        label: String(data.get('label') ?? '').trim(),
        msisdn: String(data.get('msisdn') ?? '').trim(),
        reason: String(data.get('reason') ?? '').trim()
      })
    }));
    form.reset();
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
      {message ? <div className={isFailureMessage(message) ? 'alert alert-danger' : 'alert alert-information'} role="status">{message}</div> : null}
      {transientPairing ? <div className="alert alert-warning" role="status">{transientPairing}<button className="text-button" type="button" onClick={() => setTransientPairing('')}>Dismiss</button></div> : null}

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
                <tr key={item.id} className={selectedSessionId === item.id ? 'selected-row' : undefined} onClick={() => { setSelectedSessionId(item.id); setHealth(undefined); setTransientPairing(''); }}>
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
          <div className="button-row">
            <button className="secondary compact-button" type="button" disabled={!canRead || !selected || Boolean(busy)} onClick={() => void readHealth()}>Read health</button>
            <button className="secondary compact-button" type="button" disabled={!canAdmin || !selected || Boolean(busy)} onClick={() => void requestPairing('qr')}>Request QR</button>
          </div>
          {health ? <div className="alert alert-information"><strong>Health: {health.status || 'reported'}</strong><small>Session {health.sessionId}</small></div> : null}
          <small>Pairing credentials and QR payloads are never rendered, logged, or persisted by this console.</small>
        </article>

        <article className="card">
          <h2>Pairing code</h2>
          <p className="muted">Use only for an approved pairing operation. The phone number is sent for this request and then cleared.</p>
          <form className="form-stack" onSubmit={(event) => void requestPairing('code', event)}>
            <label>Pairing phone number<input name="phoneNumber" type="tel" placeholder="+234…" autoComplete="off" required /></label>
            <label>Reason<textarea name="reason" minLength={8} rows={3} required /></label>
            <button className="primary" type="submit" disabled={!canAdmin || !selected || Boolean(busy)}>Request pairing code</button>
          </form>
        </article>
      </section>

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={registerSession}>
          <h2>Register sender session</h2>
          <p className="muted">Creates a NEW governed session. Raw MSISDN and recovery reference are write-only inputs; only masked identity and configuration state return to inventory.</p>
          <label>Owner node ID<input name="nodeId" required /></label>
          <label>Sender pool ID<select name="poolId" required defaultValue=""><option value="" disabled>Select a pool</option>{pools.map((pool) => <option key={pool.id} value={pool.id}>{pool.name} · {pool.id}</option>)}</select></label>
          <label>Gateway pool ID<input name="gatewayPoolId" required placeholder="Engine-specific gateway pool ID" /></label>
          <label>Engine<select name="engineType" defaultValue="BAILEYS"><option value="BAILEYS">Baileys</option><option value="WHATSAPP_WEB_JS">WWebJS / Chromium</option></select></label>
          <div className="grid grid-2">
            <label>Engine version<input name="engineVersion" required /></label>
            <label>State volume reference<input name="stateVolumeReference" required autoComplete="off" /></label>
          </div>
          <label>Raw E.164 MSISDN<input name="msisdn" type="tel" placeholder="+234…" autoComplete="off" required /></label>
          <div className="grid grid-2">
            <label>Owner reference<input name="ownerReference" required /></label>
            <label>Registration country<input name="registrationCountryIso2" minLength={2} maxLength={2} placeholder="NG" required /></label>
          </div>
          <label>Profile display name<input name="profileDisplayName" required /></label>
          <label>Recovery reference<input name="recoveryReference" autoComplete="off" required /></label>
          <div className="grid grid-2">
            <label>Safe messages/min<input name="safeMessagesPerMinute" type="number" min={1} required /></label>
            <label>Safe daily capacity<input name="safeDailyCapacity" type="number" min={1} required /></label>
            <label>In-flight limit<input name="inFlightLimit" type="number" min={1} max={100} required /></label>
          </div>
          <label>Reason<textarea name="reason" minLength={8} rows={3} required /></label>
          <button className="primary" type="submit" disabled={!canAdmin || Boolean(busy)}>Register NEW session</button>
        </form>

        <form className="card form-stack" key={selected ? selected.id + ':' + selected.version : 'none'} onSubmit={updateMetadata}>
          <h2>Session metadata</h2>
          <p className="muted">{selected ? 'Edit governed metadata for ' + (selected.profileDisplayName || selected.maskedMsisdn) + ' · version ' + selected.version : 'Select a session to edit metadata.'}</p>
          <label>Owner reference<input name="ownerReference" defaultValue={selected?.ownerReference || ''} required /></label>
          <label>Registration country<input name="registrationCountryIso2" defaultValue={selected?.registrationCountryIso2 || ''} minLength={2} maxLength={2} required /></label>
          <label>Profile display name<input name="profileDisplayName" defaultValue={selected?.profileDisplayName || ''} required /></label>
          <label>Recovery reference<input name="recoveryReference" autoComplete="off" required /></label>
          <label>Reason<textarea name="reason" minLength={8} rows={3} required /></label>
          <button className="primary" type="submit" disabled={!canAdmin || !selected || Boolean(busy)}>Save metadata</button>
        </form>
      </section>

      <section className="grid grid-2">
        <article className="card">
          <h2>Gateway nodes</h2>
          {nodes.length === 0 ? <div className="empty-state"><strong>No nodes</strong><p>No governed gateway runtime has registered.</p></div> : (
            <div className="table-wrap"><table><thead><tr><th>Node</th><th>Route</th><th>Status</th><th>Load</th></tr></thead><tbody>{nodes.map((node) => <tr key={node.id}><td><strong>{node.name}</strong><small>{node.adapterVersion || 'Adapter unknown'}</small></td><td>{node.provider || '—'} / {node.engine || '—'}</td><td><StatusBadge status={node.status} />{node.draining ? <small>Draining</small> : null}</td><td>{node.sessionCount} sessions<small>Queue {node.queueDepth}</small></td></tr>)}</tbody></table></div>
          )}
        </article>

        <article className="card">
          <h2>Sender pool capacity</h2>
          <p className="muted">{selectedPool ? selectedPool.name + ' · ' + selectedPool.id : 'Select Read capacity on a pool below.'}</p>
          {capacity ? <dl className="definition-list"><div><dt>READY sessions</dt><dd>{capacity.readySessions}</dd></div><div><dt>Healthy nodes</dt><dd>{capacity.healthyNodes}</dd></div><div><dt>Available messages/min</dt><dd>{capacity.availableMessagesPerMinute} / {capacity.configuredMessagesPerMinute}</dd></div><div><dt>Available daily capacity</dt><dd>{capacity.availableDailyCapacity.toLocaleString()} / {capacity.configuredDailyCapacity.toLocaleString()}</dd></div><div><dt>Reserved</dt><dd>{capacity.reservedCapacity.toLocaleString()}</dd></div><div><dt>As at</dt><dd>{new Date(capacity.asAt).toLocaleString()}</dd></div></dl> : <div className="empty-state compact-empty"><p>Capacity is read on demand to keep the inventory bounded.</p></div>}
        </article>
      </section>

      <section className="grid grid-2">
        <article className="card">
          <h2>Sender pools</h2>
          {pools.length === 0 ? <div className="empty-state"><strong>No sender pools</strong><p>Create governed capacity before campaign routing.</p></div> : (
            <div className="table-wrap"><table><thead><tr><th>Pool</th><th>Status</th><th>MPM</th><th>Daily capacity</th><th>Reserved</th><th>Actions</th></tr></thead><tbody>{pools.map((pool) => <tr key={pool.id} className={selectedPoolId === pool.id ? 'selected-row' : undefined} onClick={() => setSelectedPoolId(pool.id)}><td><strong>{pool.name}</strong><small>{pool.id}</small></td><td><StatusBadge status={pool.status} /></td><td>{pool.maxMessagesPerMinute}</td><td>{pool.dailyCapacity.toLocaleString()}</td><td>{pool.reservedCapacity.toLocaleString()}</td><td><button className="secondary compact-button" type="button" disabled={Boolean(busy)} onClick={(event) => { event.stopPropagation(); void readCapacity(pool); }}>Read capacity</button></td></tr>)}</tbody></table></div>
          )}
        </article>

        <section className="workspace-stack">
          <form className="card form-stack" onSubmit={createPool}>
            <h2>Create sender pool</h2>
            <p className="muted">Capacity is governed per sender pool. Pair each pool with one engine-specific gateway pool at campaign routing time.</p>
            <label>Name<input name="name" required /></label>
            <label>Organisation ID<input name="organisationId" /></label>
            <div className="grid grid-2"><label>Status<select name="status" defaultValue="ACTIVE"><option value="ACTIVE">Active</option><option value="PAUSED">Paused</option><option value="RETIRED">Retired</option></select></label><label>Messages/min<input name="maxMessagesPerMinute" type="number" min={1} required /></label></div>
            <div className="grid grid-2"><label>Daily capacity<input name="dailyCapacity" type="number" min={1} required /></label><label>Reserved capacity<input name="reservedCapacity" type="number" min={0} required /></label></div>
            <label>Reason<textarea name="reason" minLength={8} rows={2} required /></label>
            <button className="primary" type="submit" disabled={!canAdmin || Boolean(busy)}>Create pool</button>
          </form>

          <form className="card form-stack" key={selectedPool ? selectedPool.id + ':' + selectedPool.version : 'none'} onSubmit={updatePool}>
            <h2>Update sender pool</h2>
            <p className="muted">{selectedPool ? selectedPool.name + ' · version ' + selectedPool.version : 'Select a pool to edit.'}</p>
            <label>Name<input name="name" defaultValue={selectedPool?.name || ''} required /></label>
            <div className="grid grid-2"><label>Status<select name="status" defaultValue={selectedPool?.status || 'ACTIVE'}><option value="ACTIVE">Active</option><option value="PAUSED">Paused</option><option value="RETIRED">Retired</option></select></label><label>Messages/min<input name="maxMessagesPerMinute" type="number" min={1} defaultValue={selectedPool?.maxMessagesPerMinute || ''} required /></label></div>
            <div className="grid grid-2"><label>Daily capacity<input name="dailyCapacity" type="number" min={1} defaultValue={selectedPool?.dailyCapacity || ''} required /></label><label>Reserved capacity<input name="reservedCapacity" type="number" min={0} defaultValue={selectedPool?.reservedCapacity || ''} required /></label></div>
            <label>Reason<textarea name="reason" minLength={8} rows={2} required /></label>
            <button className="primary" type="submit" disabled={!canAdmin || !selectedPool || Boolean(busy)}>Save pool</button>
          </form>
        </section>
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
