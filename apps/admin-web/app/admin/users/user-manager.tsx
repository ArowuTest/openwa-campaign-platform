'use client';

import { FormEvent, useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../../components/auth-provider';
import { StatusBadge } from '../../../components/status-badge';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../../../lib/api';
import { hasPermission } from '../../../lib/session';

type AccountStatus = 'INVITED' | 'ACTIVE' | 'SUSPENDED' | 'DISABLED';

type InternalUser = {
  id: string;
  email: string;
  displayName: string;
  status: AccountStatus;
  mfaRequired: boolean;
  roleCodes: string[];
  version: number;
  lastLoginAt?: string | null;
  createdAt: string;
  updatedAt: string;
};

type RoleDefinition = {
  code: string;
  name: string;
  description?: string;
  systemRole: boolean;
  permissions: string[];
};

type ProvisionedAccount = {
  account: InternalUser;
  totpSecret: string;
  sessionsRevoked?: number;
};

type OneTimeSecret = {
  email: string;
  value: string;
  context: 'provisioning' | 'credential-reset';
};

const statuses: AccountStatus[] = ['INVITED', 'ACTIVE', 'SUSPENDED', 'DISABLED'];

function displayDate(value?: string | null) {
  if (!value) return 'Never';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString();
}

function messageFor(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

function isStepUp(error: unknown) {
  return error instanceof APIError && (error.code === 'STEP_UP_REQUIRED' || error.code === 'MFA_STEP_UP_REQUIRED');
}

function roleSelectionLabel(role: RoleDefinition) {
  return role.description || role.code;
}

function RoleChecklist({
  roles,
  selected,
  onToggle,
  disabled
}: {
  roles: RoleDefinition[];
  selected: string[];
  onToggle: (code: string) => void;
  disabled?: boolean;
}) {
  return (
    <fieldset>
      <legend>Roles</legend>
      {!roles.length ? <p className="muted">No assignable roles were returned by the platform.</p> : (
        <div className="form-stack">
          {roles.map((role) => (
            <label key={role.code} className="checkbox-row">
              <input
                type="checkbox"
                aria-label={role.name}
                checked={selected.includes(role.code)}
                onChange={() => onToggle(role.code)}
                disabled={disabled}
              />
              <span>
                <strong>{role.name}</strong>
                <small>{roleSelectionLabel(role)} · {role.permissions.join(', ') || 'No permissions'}</small>
              </span>
            </label>
          ))}
        </div>
      )}
    </fieldset>
  );
}

function toggle(values: string[], code: string) {
  return values.includes(code)
    ? values.filter((value) => value !== code)
    : [...values, code].sort();
}

export function UserManager() {
  const { session } = useAuth();
  const canAdminister = hasPermission(session?.permissions, 'identity.admin');
  const generation = JSON.stringify([session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  if (!canAdminister) return <p role="status">User administration requires identity.admin permission.</p>;
  return <UserManagerGeneration key={generation} />;
}

function UserManagerGeneration() {
  const router = useRouter();
  const { session } = useAuth();
  const canAdminister = hasPermission(session?.permissions, 'identity.admin');

  const [users, setUsers] = useState<InternalUser[]>([]);
  const [roles, setRoles] = useState<RoleDefinition[]>([]);
  const [selected, setSelected] = useState<InternalUser>();
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [oneTimeSecret, setOneTimeSecret] = useState<OneTimeSecret>();
  const loadRequest = useRef(0);
  const mounted = useRef(false);

  const [email, setEmail] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [temporaryPassword, setTemporaryPassword] = useState('');
  const [createRoles, setCreateRoles] = useState<string[]>([]);
  const [createReason, setCreateReason] = useState('');

  const [editDisplayName, setEditDisplayName] = useState('');
  const [editStatus, setEditStatus] = useState<AccountStatus>('INVITED');
  const [editMFARequired, setEditMFARequired] = useState(true);
  const [editRoles, setEditRoles] = useState<string[]>([]);
  const [editReason, setEditReason] = useState('');
  const [resetPassword, setResetPassword] = useState('');
  const [resetReason, setResetReason] = useState('');
  const [securityReason, setSecurityReason] = useState('');

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      loadRequest.current += 1;
    };
  }, []);

  const routeToStepUp = useCallback(() => {
    if (mounted.current) router.push('/step-up?returnTo=' + encodeURIComponent('/admin/users'));
  }, [router]);

  const load = useCallback(async () => {
    const request = ++loadRequest.current;
    try {
      const [nextUsers, nextRoles] = await Promise.all([
        collectBoundedPages(
          (cursor) => apiRequest<ListEnvelope<InternalUser>>('/v1/admin/users?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
          20
        ),
        apiRequest<RoleDefinition[]>('/v1/admin/roles')
      ]);
      if (!mounted.current || request !== loadRequest.current) return;
      setUsers(nextUsers);
      setRoles(nextRoles);
    } catch (error) {
      if (!mounted.current || request !== loadRequest.current) return;
      if (isStepUp(error)) {
        routeToStepUp();
        return;
      }
      setMessage(messageFor(error, 'User administration could not be loaded.'));
    }
  }, [routeToStepUp]);

  useEffect(() => {
    // Initial load synchronizes the server-authoritative identity register and role catalogue.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  function openUser(user: InternalUser) {
    setSelected(user);
    setEditDisplayName(user.displayName);
    setEditStatus(user.status);
    setEditMFARequired(user.mfaRequired);
    setEditRoles([...user.roleCodes].sort());
    setEditReason('');
    setResetPassword('');
    setResetReason('');
    setSecurityReason('');
    setOneTimeSecret(undefined);
    setMessage('');
  }

  async function provision(event: FormEvent) {
    event.preventDefault();
    if (!canAdminister || createRoles.length === 0 || createReason.trim().length < 5 || temporaryPassword.length < 20) return;
    setBusy('provision');
    setMessage('');
    setOneTimeSecret(undefined);
    try {
      const result = await apiRequest<ProvisionedAccount>('/v1/admin/users', {
        method: 'POST',
        body: JSON.stringify({
          email: email.trim(),
          displayName: displayName.trim(),
          temporaryPassword,
          roleCodes: createRoles,
          reason: createReason.trim()
        })
      });
      if (!mounted.current) return;
      setEmail('');
      setDisplayName('');
      setTemporaryPassword('');
      setCreateRoles([]);
      setCreateReason('');
      openUser(result.account);
      setOneTimeSecret({ email: result.account.email, value: result.totpSecret, context: 'provisioning' });
      setMessage('Internal user provisioned. Complete the controlled MFA enrolment handoff before dismissing the secret.');
      await load();
    } catch (error) {
      if (!mounted.current) return;
      if (isStepUp(error)) {
        routeToStepUp();
        return;
      }
      setMessage(messageFor(error, 'Internal user could not be provisioned.'));
    } finally {
      if (mounted.current) setBusy('');
    }
  }

  async function saveUser(event: FormEvent) {
    event.preventDefault();
    if (!selected || !canAdminister || editRoles.length === 0 || editReason.trim().length < 5) return;
    setBusy('save');
    setMessage('');
    try {
      const updated = await apiRequest<InternalUser>('/v1/admin/users/' + encodeURIComponent(selected.id), {
        method: 'PUT',
        body: JSON.stringify({
          displayName: editDisplayName.trim(),
          status: editStatus,
          roleCodes: editRoles,
          mfaRequired: editStatus === 'ACTIVE' ? true : editMFARequired,
          expectedVersion: selected.version,
          reason: editReason.trim()
        })
      });
      if (!mounted.current) return;
      openUser(updated);
      setMessage('User access policy updated.');
      await load();
    } catch (error) {
      if (!mounted.current) return;
      if (isStepUp(error)) {
        routeToStepUp();
        return;
      }
      setMessage(messageFor(error, 'User access policy could not be updated.'));
    } finally {
      if (mounted.current) setBusy('');
    }
  }

  async function resetCredentials() {
    if (!selected || !canAdminister || resetPassword.length < 20 || resetReason.trim().length < 5) return;
    setBusy('reset');
    setMessage('');
    setOneTimeSecret(undefined);
    try {
      const result = await apiRequest<ProvisionedAccount>('/v1/admin/users/' + encodeURIComponent(selected.id) + '/reset-credentials', {
        method: 'POST',
        body: JSON.stringify({
          temporaryPassword: resetPassword,
          expectedVersion: selected.version,
          reason: resetReason.trim()
        })
      });
      if (!mounted.current) return;
      setResetPassword('');
      setResetReason('');
      openUser(result.account);
      setOneTimeSecret({ email: result.account.email, value: result.totpSecret, context: 'credential-reset' });
      setMessage('Credentials reset and active sessions revoked. Complete the new MFA enrolment handoff.');
      await load();
    } catch (error) {
      if (!mounted.current) return;
      if (isStepUp(error)) {
        routeToStepUp();
        return;
      }
      setMessage(messageFor(error, 'Credentials could not be reset.'));
    } finally {
      if (mounted.current) setBusy('');
    }
  }

  async function unlock() {
    if (!selected || !canAdminister || securityReason.trim().length < 5) return;
    setBusy('unlock');
    setMessage('');
    try {
      const updated = await apiRequest<InternalUser>('/v1/admin/users/' + encodeURIComponent(selected.id) + '/unlock', {
        method: 'POST',
        body: JSON.stringify({ expectedVersion: selected.version, reason: securityReason.trim() })
      });
      if (!mounted.current) return;
      openUser(updated);
      setMessage('Login lock state cleared.');
      await load();
    } catch (error) {
      if (!mounted.current) return;
      if (isStepUp(error)) {
        routeToStepUp();
        return;
      }
      setMessage(messageFor(error, 'Login lock state could not be cleared.'));
    } finally {
      if (mounted.current) setBusy('');
    }
  }

  async function revokeSessions() {
    if (!selected || !canAdminister || securityReason.trim().length < 5) return;
    setBusy('revoke');
    setMessage('');
    try {
      const result = await apiRequest<{ revoked: number }>('/v1/admin/users/' + encodeURIComponent(selected.id) + '/revoke-sessions', {
        method: 'POST',
        body: JSON.stringify({ reason: securityReason.trim() })
      });
      if (!mounted.current) return;
      setSecurityReason('');
      setMessage(result.revoked === 1 ? '1 active session revoked.' : result.revoked + ' active sessions revoked.');
    } catch (error) {
      if (!mounted.current) return;
      if (isStepUp(error)) {
        routeToStepUp();
        return;
      }
      setMessage(messageFor(error, 'Active sessions could not be revoked.'));
    } finally {
      if (mounted.current) setBusy('');
    }
  }

  return (
    <div className="workspace-stack">
      {message ? <div className="alert alert-information" role="status">{message}</div> : null}

      {oneTimeSecret ? (
        <section className="card form-stack" aria-labelledby="one-time-secret-title">
          <div className="section-heading">
            <div>
              <h2 id="one-time-secret-title">One-time MFA enrolment handoff</h2>
              <p className="muted">This secret is returned once by the server and exists only in this page state. Transfer it through the approved operator-enrolment channel, then dismiss it.</p>
            </div>
            <span className="pill">{oneTimeSecret.context === 'provisioning' ? 'New user' : 'Credential reset'}</span>
          </div>
          <dl className="definition-list">
            <div><dt>User</dt><dd>{oneTimeSecret.email}</dd></div>
            <div><dt>Secret</dt><dd><code aria-label="One-time MFA enrolment secret">{oneTimeSecret.value}</code></dd></div>
          </dl>
          <button type="button" onClick={() => setOneTimeSecret(undefined)}>Dismiss secret</button>
        </section>
      ) : null}

      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Internal user register</h2>
            <p className="muted">Named accounts only. Roles, MFA, optimistic versions and session controls remain server-authoritative.</p>
          </div>
          <span className="pill">{users.length} users</span>
        </div>
        {!users.length ? <div className="empty-state"><strong>No internal users</strong><p>Provision the first approved operator below.</p></div> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>User</th><th>Status</th><th>Roles</th><th>MFA</th><th>Last login</th><th>Version</th></tr></thead>
              <tbody>
                {users.map((user) => (
                  <tr key={user.id} className={selected?.id === user.id ? 'selected-row' : undefined}>
                    <td>
                      <button type="button" className="table-link" onClick={() => openUser(user)}>
                        {user.displayName}<small>{user.email}</small>
                      </button>
                    </td>
                    <td><StatusBadge status={user.status} /></td>
                    <td>{user.roleCodes.join(', ')}</td>
                    <td>{user.mfaRequired ? 'Required' : 'Not required'}</td>
                    <td>{displayDate(user.lastLoginAt)}</td>
                    <td>{user.version}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={provision}>
          <h2>Provision internal user</h2>
          <p className="muted">Shared or generic accounts are rejected by the platform. New users start invited with mandatory MFA.</p>
          <label>Email<input type="email" value={email} onChange={(event) => setEmail(event.target.value)} autoComplete="off" required disabled={!canAdminister} /></label>
          <label htmlFor="provision-display-name">Display name</label>
          <input id="provision-display-name" aria-describedby="provision-display-name-hint" value={displayName} onChange={(event) => setDisplayName(event.target.value)} minLength={2} maxLength={200} required disabled={!canAdminister} />
          <small id="provision-display-name-hint">Use the operator&apos;s individual name, 2 to 200 characters.</small>
          <label htmlFor="provision-password">Temporary password</label>
          <input id="provision-password" aria-describedby="provision-password-hint" type="password" value={temporaryPassword} onChange={(event) => setTemporaryPassword(event.target.value)} minLength={20} autoComplete="new-password" required disabled={!canAdminister} />
          <small id="provision-password-hint">At least 20 characters. This temporary password is cleared from the form after successful provisioning.</small>
          <RoleChecklist roles={roles} selected={createRoles} onToggle={(code) => setCreateRoles((current) => toggle(current, code))} disabled={!canAdminister} />
          <label htmlFor="provision-reason">Provisioning reason</label>
          <textarea id="provision-reason" aria-describedby="provision-reason-hint" value={createReason} onChange={(event) => setCreateReason(event.target.value)} minLength={5} maxLength={1000} required disabled={!canAdminister} />
          <small id="provision-reason-hint">Required audit reason, 5 to 1,000 characters.</small>
          <button className="primary" type="submit" disabled={!canAdminister || busy !== '' || createRoles.length === 0 || temporaryPassword.length < 20 || createReason.trim().length < 5}>
            {busy === 'provision' ? 'Provisioning…' : 'Provision user'}
          </button>
        </form>

        <form className="card form-stack" onSubmit={saveUser}>
          <div className="section-heading">
            <div>
              <h2>User access and lifecycle</h2>
              <p className="muted">{selected ? selected.email : 'Select a user to edit their access policy.'}</p>
            </div>
            {selected ? <StatusBadge status={selected.status} /> : null}
          </div>
          {!selected ? <div className="empty-state"><strong>No user selected</strong><p>Select a named operator from the register.</p></div> : (
            <>
              <label htmlFor="edit-display-name">Display name</label>
              <input id="edit-display-name" aria-describedby="edit-display-name-hint" value={editDisplayName} onChange={(event) => setEditDisplayName(event.target.value)} minLength={2} maxLength={200} required disabled={!canAdminister} />
              <small id="edit-display-name-hint">Use the operator&apos;s individual name, 2 to 200 characters.</small>
              <label>Account status<select value={editStatus} onChange={(event) => {
                const status = event.target.value as AccountStatus;
                setEditStatus(status);
                if (status === 'ACTIVE') setEditMFARequired(true);
              }} disabled={!canAdminister}>{statuses.map((status) => <option key={status} value={status}>{status.replaceAll('_', ' ')}</option>)}</select></label>
              <label className="checkbox-row">
                <input type="checkbox" checked={editMFARequired} onChange={(event) => setEditMFARequired(event.target.checked)} disabled={!canAdminister || editStatus === 'ACTIVE'} />
                <span><strong>MFA required</strong><small>Active accounts always require MFA; the server rejects downgrade attempts.</small></span>
              </label>
              <RoleChecklist roles={roles} selected={editRoles} onToggle={(code) => setEditRoles((current) => toggle(current, code))} disabled={!canAdminister} />
              <label htmlFor="edit-reason">Change reason</label>
              <textarea id="edit-reason" aria-describedby="edit-reason-hint" value={editReason} onChange={(event) => setEditReason(event.target.value)} minLength={5} maxLength={1000} required disabled={!canAdminister} />
              <small id="edit-reason-hint">Required audit reason, 5 to 1,000 characters.</small>
              <button className="primary" type="submit" disabled={!canAdminister || busy !== '' || editRoles.length === 0 || editReason.trim().length < 5}>{busy === 'save' ? 'Saving…' : 'Save user'}</button>

              <fieldset>
                <legend>Credential recovery</legend>
                <p className="muted">Resetting credentials creates a new one-time MFA secret and revokes active sessions.</p>
                <label htmlFor="reset-password">New temporary password</label>
                <input id="reset-password" aria-describedby="reset-password-hint" type="password" value={resetPassword} onChange={(event) => setResetPassword(event.target.value)} minLength={20} autoComplete="new-password" disabled={!canAdminister} />
                <small id="reset-password-hint">At least 20 characters. Successful reset revokes active sessions and returns a new one-time MFA enrolment secret.</small>
                <label htmlFor="reset-reason">Credential reset reason</label>
                <textarea id="reset-reason" aria-describedby="reset-reason-hint" value={resetReason} onChange={(event) => setResetReason(event.target.value)} minLength={5} maxLength={1000} disabled={!canAdminister} />
                <small id="reset-reason-hint">Required audit reason, 5 to 1,000 characters.</small>
                <button type="button" onClick={() => void resetCredentials()} disabled={!canAdminister || busy !== '' || resetPassword.length < 20 || resetReason.trim().length < 5}>{busy === 'reset' ? 'Resetting…' : 'Reset credentials'}</button>
              </fieldset>

              <fieldset>
                <legend>Session and lock controls</legend>
                <label htmlFor="security-reason">Security action reason</label>
                <textarea id="security-reason" aria-describedby="security-reason-hint" value={securityReason} onChange={(event) => setSecurityReason(event.target.value)} minLength={5} maxLength={1000} disabled={!canAdminister} />
                <small id="security-reason-hint">Required audit reason, 5 to 1,000 characters, for unlock or session revocation.</small>
                <div className="button-row">
                  <button type="button" onClick={() => void unlock()} disabled={!canAdminister || busy !== '' || securityReason.trim().length < 5}>{busy === 'unlock' ? 'Unlocking…' : 'Clear login lock'}</button>
                  <button type="button" onClick={() => void revokeSessions()} disabled={!canAdminister || busy !== '' || securityReason.trim().length < 5}>{busy === 'revoke' ? 'Revoking…' : 'Revoke active sessions'}</button>
                </div>
              </fieldset>

              <dl className="definition-list">
                <div><dt>User ID</dt><dd>{selected.id}</dd></div>
                <div><dt>Version</dt><dd>{selected.version}</dd></div>
                <div><dt>Created</dt><dd>{displayDate(selected.createdAt)}</dd></div>
                <div><dt>Updated</dt><dd>{displayDate(selected.updatedAt)}</dd></div>
              </dl>
            </>
          )}
        </form>
      </section>
    </div>
  );
}
