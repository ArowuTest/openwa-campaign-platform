'use client';

import { useEffect, useMemo, useRef, useState } from 'react';

import { apiRequest, collectBoundedPages, type ListEnvelope } from '../lib/api';
import { SegmentSnapshotManager, type SavedSegment } from './segment-snapshot-manager';
import {
  addChildGroup,
  addRuleToGroup,
  buildCreateSegmentRequest,
  buildEstimateRequest,
  buildFilterGroup,
  countFilterRules,
  createFilterGroupNode,
  createRule,
  filterGroupDepth,
  hydrateEditableFilterGroup,
  maxFilterGroupDepth,
  maxFilterRules,
  removeChildGroup,
  removeRuleFromGroup,
  requireAuthoritativeCatalogue,
  updateGroupJoin,
  updateRuleInGroup,
  type CohortEstimate,
  type FilterDefinition,
  type EditableFilterGroup,
  type GeographyItem,
  type Rule
} from '../lib/audience-builder-model';

type CatalogueState = {
  state: 'loading' | 'ready' | 'error';
  message: string;
};

type ValidationState = {
  state: 'idle' | 'loading' | 'valid' | 'invalid';
  message: string;
};

type CohortEstimateJob = {
  id: string;
  organisationId: string;
  purposeId: string;
  channel: 'WHATSAPP';
  asOf: string;
  clientRequestId: string;
  status: 'PENDING' | 'PROCESSING' | 'COMPLETED' | 'DEAD_LETTER' | 'CANCELLED';
  attemptCount: number;
  maxAttempts: number;
  lastErrorCode?: string;
  result?: Omit<CohortEstimate, 'asOf'>;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
};

type EstimateState = {
  state: 'idle' | 'loading' | 'pending' | 'ready' | 'error';
  message: string;
  value?: CohortEstimate;
  job?: CohortEstimateJob;
};

function estimateStateFromJob(job: CohortEstimateJob): EstimateState {
  if (job.status === 'COMPLETED') {
    if (!job.result) {
      return { state: 'error', job, message: 'The estimate completed without a durable result. Refresh or contact an administrator.' };
    }
    return {
      state: 'ready',
      job,
      value: { ...job.result, asOf: job.asOf },
      message: job.result.breakdown
        ? 'Authoritative eligibility waterfall completed in the durable audience worker.'
        : 'Final eligible count completed; intermediate governance stages were not reported.'
    };
  }
  if (job.status === 'DEAD_LETTER' || job.status === 'CANCELLED') {
    return {
      state: 'error',
      job,
      message: job.lastErrorCode
        ? `Durable estimate stopped with ${job.lastErrorCode}. Review the request before retrying.`
        : 'Durable estimate stopped before a result was produced.'
    };
  }
  return {
    state: 'pending',
    job,
    message: job.status === 'PROCESSING'
      ? `Calculating authoritative eligibility stages · attempt ${job.attemptCount} of ${job.maxAttempts}.`
      : 'Estimate queued durably. It can recover from worker restarts without recomputing in this browser request.'
  };
}

type SaveState = {
  state: 'idle' | 'saving' | 'saved' | 'error';
  message: string;
};

type Organisation = {
  id: string;
  legalName: string;
  tradingName?: string;
  status: string;
};

type ConsentPurpose = {
  id: string;
  organisationId: string;
  code: string;
  name: string;
  description?: string;
  channel: string;
  wordingVersion: string;
  active: boolean;
};

type GeographyCache = {
  states: Record<string, GeographyItem[]>;
  lgas: Record<string, GeographyItem[]>;
};

function inputType(definition: FilterDefinition): 'number' | 'date' | 'text' {
  if (definition.dataType === 'integer' || definition.dataType === 'decimal') return 'number';
  if (definition.dataType === 'date') return 'date';
  return 'text';
}

function initialValues(operator: string): string[] {
  if (operator === 'between') return ['', ''];
  if (operator === 'is_known' || operator === 'is_unknown') return [];
  return [''];
}

function maxTreeId(group: EditableFilterGroup): number {
  return Math.max(
    group.id,
    ...group.rules.map((rule) => rule.id),
    ...group.children.map(maxTreeId)
  );
}

function localValue(group: EditableFilterGroup, definitionCode: string): string {
  return group.rules.find((rule) => rule.definitionCode === definitionCode)?.values[0] ?? '';
}

function geographyRequests(
  group: EditableFilterGroup,
  inheritedCountry = '',
  inheritedState = '',
  states = new Set<string>(),
  lgas = new Set<string>()
): { states: string[]; lgas: string[] } {
  const country = localValue(group, 'COUNTRY') || inheritedCountry;
  const state = localValue(group, 'STATE') || inheritedState;
  if (country) states.add(country);
  if (country && state) lgas.add(country + '\u001f' + state);
  for (const child of group.children) geographyRequests(child, country, state, states, lgas);
  return { states: [...states].sort(), lgas: [...lgas].sort() };
}

function readableDefinition(group: EditableFilterGroup, definitions: readonly FilterDefinition[]): string {
  const parts: string[] = [];
  for (const rule of group.rules) {
    const definition = definitions.find((item) => item.code === rule.definitionCode);
    const values = rule.values.filter(Boolean).join(' and ');
    parts.push(
      (definition?.displayName ?? rule.definitionCode) +
      ' ' +
      rule.operator.replaceAll('_', ' ') +
      (values ? ' ' + values : '')
    );
  }
  for (const child of group.children) parts.push('(' + readableDefinition(child, definitions) + ')');
  return parts.join(' ' + group.join + ' ');
}

export function FilterBuilder() {
  const [definitions, setDefinitions] = useState<FilterDefinition[]>([]);
  const [root, setRoot] = useState<EditableFilterGroup>(() => createFilterGroupNode(1));
  const [countries, setCountries] = useState<GeographyItem[]>([]);
  const [geographyCache, setGeographyCache] = useState<GeographyCache>({ states: {}, lgas: {} });
  const [catalogue, setCatalogue] = useState<CatalogueState>({
    state: 'loading',
    message: 'Loading authoritative filter, geography and organisation catalogues…'
  });
  const [catalogueAttempt, setCatalogueAttempt] = useState(0);
  const [geographyError, setGeographyError] = useState('');
  const [validation, setValidation] = useState<ValidationState>({ state: 'idle', message: '' });
  const [estimate, setEstimate] = useState<EstimateState>({ state: 'idle', message: '' });
  const [organisations, setOrganisations] = useState<Organisation[]>([]);
  const [purposes, setPurposes] = useState<ConsentPurpose[]>([]);
  const [organisationId, setOrganisationId] = useState('');
  const [purposeId, setPurposeId] = useState('');
  const [segmentName, setSegmentName] = useState('');
  const [segmentDescription, setSegmentDescription] = useState('');
  const [saveState, setSaveState] = useState<SaveState>({ state: 'idle', message: '' });
  const [loadedSegmentId, setLoadedSegmentId] = useState('');
  const [segmentRefreshToken, setSegmentRefreshToken] = useState(0);
  const nextIdRef = useRef(10);
  const contextGeneration = useRef(0);
  const validationRequest = useRef(0);
  const estimateRequest = useRef(0);
  const saveRequest = useRef(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const definitionPayload = useMemo(() => {
    try {
      return buildFilterGroup(root, definitions);
    } catch {
      return undefined;
    }
  }, [definitions, root]);

  const geoRequests = useMemo(() => geographyRequests(root), [root]);
  const summary = useMemo(() => readableDefinition(root, definitions), [definitions, root]);
  const selectedPurpose = purposes.find((item) => item.id === purposeId);
  const ruleCount = countFilterRules(root);
  const depth = filterGroupDepth(root);

  useEffect(() => {
    if (estimate.state !== 'pending' || !estimate.job?.id || !organisationId) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const jobId = estimate.job.id;
    const generation = contextGeneration.current;
    const request = estimateRequest.current;
    const isCurrent = () => !cancelled && mounted.current &&
      generation === contextGeneration.current && request === estimateRequest.current;
    const poll = async () => {
      try {
        const job = await apiRequest<CohortEstimateJob>(
          `/v1/cohort-estimates/${encodeURIComponent(jobId)}?organisationId=${encodeURIComponent(organisationId)}`
        );
        if (!isCurrent()) return;
        const next = estimateStateFromJob(job);
        setEstimate(next);
        if (next.state === 'pending') timer = setTimeout(poll, 1500);
      } catch (cause) {
        if (!isCurrent()) return;
        setEstimate({
          state: 'error',
          message: cause instanceof Error ? cause.message : 'Durable estimate status could not be recovered.'
        });
      }
    };
    timer = setTimeout(poll, 800);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [estimate.state, estimate.job?.id, organisationId]);

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      apiRequest<ListEnvelope<FilterDefinition>>('/v1/filter-definitions'),
      apiRequest<ListEnvelope<GeographyItem>>('/v1/geography/countries'),
      collectBoundedPages<Organisation>(
        (cursor) => apiRequest<ListEnvelope<Organisation>>(
          '/v1/organisations?limit=500' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
        ),
        20
      )
    ]).then(([definitionPayload, countryPayload, organisationItems]) => {
      if (cancelled) return;
      const authoritative = requireAuthoritativeCatalogue(definitionPayload.items, countryPayload.items);
      const initial = createFilterGroupNode(1);
      const seeded = addRuleToGroup(initial, initial.id, createRule(authoritative.definitions[0], 2));
      setDefinitions(authoritative.definitions);
      setCountries(authoritative.countries);
      setOrganisations(organisationItems.filter((item) => item.status === 'ACTIVE'));
      setRoot(seeded);
      nextIdRef.current = maxTreeId(seeded) + 1;
      setGeographyCache({ states: {}, lgas: {} });
      setCatalogue({ state: 'ready', message: '' });
      setGeographyError('');
      setValidation({ state: 'idle', message: '' });
      setEstimate({ state: 'idle', message: '' });
    }).catch((cause) => {
      if (cancelled) return;
      setDefinitions([]);
      setCountries([]);
      setOrganisations([]);
      setCatalogue({
        state: 'error',
        message: cause instanceof Error
          ? cause.message
          : 'The authoritative audience catalogues could not be loaded.'
      });
    });
    return () => { cancelled = true; };
  }, [catalogueAttempt]);

  useEffect(() => {
    if (!organisationId) return;
    let cancelled = false;
    void collectBoundedPages<ConsentPurpose>(
      (cursor) => apiRequest<ListEnvelope<ConsentPurpose>>(
        '/v1/consent-purposes?organisationId=' + encodeURIComponent(organisationId) +
        '&limit=500' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')
      ),
      20
    ).then((items) => {
      if (!cancelled) {
        setPurposes(items.filter((item) => item.active && item.channel === 'WHATSAPP'));
      }
    }).catch((cause) => {
      if (!cancelled) setEstimate({
        state: 'error',
        message: cause instanceof Error ? cause.message : 'Consent-purpose inventory could not be loaded.'
      });
    });
    return () => { cancelled = true; };
  }, [organisationId]);

  useEffect(() => {
    if (catalogue.state !== 'ready') return;
    let cancelled = false;
    const stateCountries = geoRequests.states;
    const lgaParents = geoRequests.lgas;
    void Promise.all([
      ...stateCountries.map(async (country) => {
        const payload = await apiRequest<ListEnvelope<GeographyItem>>(
          '/v1/geography/areas?country=' + encodeURIComponent(country) + '&level=1'
        );
        return { kind: 'state' as const, key: country, items: payload.items ?? [] };
      }),
      ...lgaParents.map(async (key) => {
        const [country, state] = key.split('\u001f');
        const payload = await apiRequest<ListEnvelope<GeographyItem>>(
          '/v1/geography/areas?country=' + encodeURIComponent(country) +
          '&parent=' + encodeURIComponent(state) + '&level=2'
        );
        return { kind: 'lga' as const, key, items: payload.items ?? [] };
      })
    ]).then((results) => {
      if (cancelled) return;
      setGeographyCache((current) => {
        const states = { ...current.states };
        const lgas = { ...current.lgas };
        for (const item of results) {
          if (item.kind === 'state') states[item.key] = item.items;
          else lgas[item.key] = item.items;
        }
        return { states, lgas };
      });
      setGeographyError('');
    }).catch((cause) => {
      if (!cancelled) setGeographyError(
        cause instanceof Error ? cause.message : 'A governed geography catalogue could not be loaded.'
      );
    });
    return () => { cancelled = true; };
  }, [catalogue.state, geoRequests]);

  function resetEvidence() {
    // A submitted mutation can still commit server-side; only its obsolete UI evidence is discarded.
    contextGeneration.current += 1;
    validationRequest.current += 1;
    estimateRequest.current += 1;
    saveRequest.current += 1;
    setValidation({ state: 'idle', message: '' });
    setEstimate({ state: 'idle', message: '' });
    setSaveState({ state: 'idle', message: '' });
  }

  function retryCatalogue() {
    setDefinitions([]);
    setCountries([]);
    setOrganisations([]);
    setPurposes([]);
    setRoot(createFilterGroupNode(1));
    setCatalogue({ state: 'loading', message: 'Reloading authoritative audience catalogues…' });
    setGeographyError('');
    resetEvidence();
    setCatalogueAttempt((value) => value + 1);
  }

  function nextId(): number {
    const value = nextIdRef.current;
    nextIdRef.current += 1;
    return value;
  }

  function updateRule(groupId: number, ruleId: number, patch: Partial<Rule>) {
    setRoot((current) => updateRuleInGroup(current, groupId, ruleId, (rule) => ({ ...rule, ...patch })));
    setGeographyError('');
    resetEvidence();
  }

  function addRule(groupId: number) {
    const definition = definitions[0];
    if (!definition) return;
    try {
      setRoot((current) => addRuleToGroup(current, groupId, createRule(definition, nextId())));
      resetEvidence();
    } catch (cause) {
      setValidation({ state: 'invalid', message: cause instanceof Error ? cause.message : 'Unable to add filter.' });
    }
  }

  function addGroup(parentId: number) {
    try {
      setRoot((current) => addChildGroup(current, parentId, createFilterGroupNode(nextId())));
      resetEvidence();
    } catch (cause) {
      setValidation({ state: 'invalid', message: cause instanceof Error ? cause.message : 'Unable to add group.' });
    }
  }

  function removeGroup(groupId: number) {
    try {
      setRoot((current) => removeChildGroup(current, groupId));
      resetEvidence();
    } catch (cause) {
      setValidation({ state: 'invalid', message: cause instanceof Error ? cause.message : 'Unable to remove group.' });
    }
  }

  function openSavedSegment(segment: SavedSegment) {
    try {
      const hydrated = hydrateEditableFilterGroup(segment.definition, nextIdRef.current);
      setRoot(hydrated.group);
      nextIdRef.current = hydrated.nextId;
      setSegmentName(segment.name);
      setSegmentDescription(segment.description ?? '');
      setLoadedSegmentId(segment.id);
      resetEvidence();
      setSaveState({ state: 'saved', message: `Loaded ${segment.name} v${segment.version} into the editor.` });
    } catch (cause) {
      setValidation({ state: 'invalid', message: cause instanceof Error ? cause.message : 'Saved segment could not be loaded.' });
    }
  }

  async function validate() {
    const generation = contextGeneration.current;
    const request = ++validationRequest.current;
    const isCurrent = () => mounted.current && generation === contextGeneration.current && request === validationRequest.current;
    let authoritative;
    try {
      authoritative = buildFilterGroup(root, definitions);
    } catch (cause) {
      setValidation({ state: 'invalid', message: cause instanceof Error ? cause.message : 'Cohort definition is invalid.' });
      return;
    }
    setValidation({ state: 'loading', message: 'Validating cohort definition against the authoritative registry…' });
    estimateRequest.current += 1;
    setEstimate({ state: 'idle', message: '' });
    try {
      await apiRequest('/v1/cohorts/validate', {
        method: 'POST',
        body: JSON.stringify(authoritative)
      });
      if (!isCurrent()) return;
      setValidation({
        state: 'valid',
        message: 'Cohort definition is valid for the current governed filter registry and operator permissions.'
      });
    } catch (cause) {
      if (!isCurrent()) return;
      setValidation({
        state: 'invalid',
        message: cause instanceof Error ? cause.message : 'Cohort definition is invalid.'
      });
    }
  }

  async function estimateAudience() {
    const generation = contextGeneration.current;
    const action = ++estimateRequest.current;
    const isCurrent = () => mounted.current && generation === contextGeneration.current && action === estimateRequest.current;
    const clientRequestId =
      typeof globalThis.crypto?.randomUUID === 'function'
        ? globalThis.crypto.randomUUID()
        : `estimate-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
    try {
      const definition = buildFilterGroup(root, definitions);
      const request = buildEstimateRequest(definition, {
        organisationId,
        purposeId,
        channel: 'WHATSAPP',
        clientRequestId
      });
      setEstimate({ state: 'loading', message: 'Scheduling restart-safe authoritative eligibility estimate…' });
      const job = await apiRequest<CohortEstimateJob>('/v1/cohort-estimates', {
        method: 'POST',
        body: JSON.stringify(request)
      }, { idempotencyKey: clientRequestId });
      if (!isCurrent()) return;
      setEstimate(estimateStateFromJob(job));
    } catch (cause) {
      if (!isCurrent()) return;
      try {
        const recovered = await collectBoundedPages<CohortEstimateJob>(
          (cursor) =>
            apiRequest<ListEnvelope<CohortEstimateJob>>(
              `/v1/cohort-estimates?organisationId=${encodeURIComponent(organisationId)}&limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`
            ),
          2
        );
        if (!isCurrent()) return;
        const job = recovered.find((item) => item.clientRequestId === clientRequestId);
        if (job) {
          setEstimate(estimateStateFromJob(job));
          return;
        }
      } catch {
        // Preserve the original scheduling error when recovery inventory is also unavailable.
      }
      if (!isCurrent()) return;
      setEstimate({
        state: 'error',
        message: cause instanceof Error ? cause.message : 'Audience estimate could not be scheduled.'
      });
    }
  }

  async function saveSegment() {
    const generation = contextGeneration.current;
    const action = ++saveRequest.current;
    const isCurrent = () => mounted.current && generation === contextGeneration.current && action === saveRequest.current;
    try {
      const definition = buildFilterGroup(root, definitions);
      const request = buildCreateSegmentRequest(definition, {
        organisationId,
        name: segmentName,
        description: segmentDescription
      });
      setSaveState({ state: 'saving', message: 'Saving governed segment…' });
      const saved = await apiRequest<{ id: string; name: string; version: number }>('/v1/segments', {
        method: 'POST',
        body: JSON.stringify(request)
      });
      if (!isCurrent()) return;
      setLoadedSegmentId(saved.id);
      setSegmentRefreshToken((value) => value + 1);
      setSaveState({
        state: 'saved',
        message: `Saved ${saved.name} as segment ${saved.id} · version ${saved.version}.`
      });
    } catch (cause) {
      if (!isCurrent()) return;
      setSaveState({
        state: 'error',
        message: cause instanceof Error ? cause.message : 'Segment could not be saved.'
      });
    }
  }

  function optionsFor(
    definition: FilterDefinition,
    country: string,
    state: string
  ): GeographyItem[] | string[] {
    if (definition.code === 'COUNTRY') return countries;
    if (definition.code === 'STATE') return country ? (geographyCache.states[country] ?? []) : [];
    if (definition.code === 'LGA') return country && state ? (geographyCache.lgas[country + '\u001f' + state] ?? []) : [];
    return definition.allowedValues ?? [];
  }

  function renderRule(rule: Rule, groupId: number, country: string, state: string) {
    const definition = definitions.find((item) => item.code === rule.definitionCode);
    if (!definition) {
      return (
        <div className="alert alert-danger" role="alert" key={rule.id}>
          Filter definition {rule.definitionCode} is no longer available. Reload the authoritative catalogue.
        </div>
      );
    }
    const options = optionsFor(definition, country, state);
    const isBetween = rule.operator === 'between';
    const isNoValue = rule.operator === 'is_known' || rule.operator === 'is_unknown';
    const valueType = inputType(definition);
    return (
      <div className="rule" key={rule.id}>
        <label>
          <span className="field-label">Field</span>
          <select value={rule.definitionCode} onChange={(event) => {
            const next = definitions.find((item) => item.code === event.target.value);
            if (!next) return;
            const operator = next.operators[0];
            updateRule(groupId, rule.id, {
              definitionCode: next.code,
              operator,
              values: initialValues(operator)
            });
          }}>
            {definitions.map((item) => <option value={item.code} key={item.code}>{item.displayName}</option>)}
          </select>
          {definition.sensitive ? <small className="sensitive-label">Sensitive field — server permission enforced</small> : null}
        </label>

        <label>
          <span className="field-label">Operator</span>
          <select value={rule.operator} onChange={(event) => updateRule(groupId, rule.id, {
            operator: event.target.value,
            values: initialValues(event.target.value)
          })}>
            {definition.operators.map((operator) => (
              <option value={operator} key={operator}>{operator.replaceAll('_', ' ')}</option>
            ))}
          </select>
        </label>

        <div>
          <span className="field-label">Value</span>
          {isNoValue ? <div className="value-placeholder">No value required</div> : options.length > 0 ? (
            <select aria-label={`${definition.displayName} value`} value={rule.values[0] ?? ''} onChange={(event) => updateRule(groupId, rule.id, { values: [event.target.value] })}>
              <option value="">Select a governed value</option>
              {options.map((option) => typeof option === 'string'
                ? <option value={option} key={option}>{option.replaceAll('_', ' ')}</option>
                : <option value={option.iso2 ?? option.code} key={option.iso2 ?? option.code}>{option.name}</option>
              )}
            </select>
          ) : isBetween ? (
            <div className="range-inputs">
              <input
                type={valueType}
                step={definition.dataType === 'decimal' ? 'any' : undefined}
                min={definition.dataType === 'integer' ? 0 : undefined}
                max={definition.code === 'REPORTED_AGE' ? 130 : undefined}
                value={rule.values[0] ?? ''}
                onChange={(event) => updateRule(groupId, rule.id, { values: [event.target.value, rule.values[1] ?? ''] })}
                aria-label={`${definition.displayName} lower bound`}
              />
              <span>to</span>
              <input
                type={valueType}
                step={definition.dataType === 'decimal' ? 'any' : undefined}
                min={definition.dataType === 'integer' ? 0 : undefined}
                max={definition.code === 'REPORTED_AGE' ? 130 : undefined}
                value={rule.values[1] ?? ''}
                onChange={(event) => updateRule(groupId, rule.id, { values: [rule.values[0] ?? '', event.target.value] })}
                aria-label={`${definition.displayName} upper bound`}
              />
            </div>
          ) : definition.dataType === 'boolean' ? (
            <select aria-label={`${definition.displayName} value`} value={rule.values[0] ?? ''} onChange={(event) => updateRule(groupId, rule.id, { values: [event.target.value] })}>
              <option value="">Select a value</option>
              <option value="true">True</option>
              <option value="false">False</option>
            </select>
          ) : (
            <input
              type={valueType}
              step={definition.dataType === 'decimal' ? 'any' : undefined}
              min={definition.dataType === 'integer' ? 0 : undefined}
              max={definition.code === 'REPORTED_AGE' ? 130 : undefined}
              value={rule.values[0] ?? ''}
              onChange={(event) => updateRule(groupId, rule.id, { values: [event.target.value] })}
              aria-label={`${definition.displayName} value`}
              placeholder={definition.code === 'STATE' && !country
                ? 'Select a country in this or a parent group first'
                : definition.code === 'LGA' && (!country || !state)
                  ? 'Select country and state in this or a parent group first'
                  : 'Enter a value'}
            />
          )}
        </div>

        <button
          className="danger-ghost"
          type="button"
          onClick={() => {
            setRoot((current) => removeRuleFromGroup(current, groupId, rule.id));
            resetEvidence();
          }}
          aria-label={`Remove ${definition.displayName} filter`}
        >
          Remove
        </button>
      </div>
    );
  }

  function renderGroup(
    group: EditableFilterGroup,
    depthIndex: number,
    inheritedCountry = '',
    inheritedState = ''
  ): React.ReactNode {
    const country = localValue(group, 'COUNTRY') || inheritedCountry;
    const state = localValue(group, 'STATE') || inheritedState;
    return (
      <section
        className={'filter-group filter-group-depth-' + Math.min(depthIndex, 5)}
        key={group.id}
        aria-label={`Filter group depth ${depthIndex}`}
      >
        <div className="filter-group-header">
          <div>
            <span className="eyebrow">{depthIndex === 1 ? 'Root group' : `Nested group · depth ${depthIndex}`}</span>
            <strong>{group.join === 'AND' ? 'All conditions must match' : 'Any condition may match'}</strong>
          </div>
          <div className="button-row">
            <label className="inline-select">Join
              <select
                value={group.join}
                onChange={(event) => {
                  setRoot((current) => updateGroupJoin(current, group.id, event.target.value as 'AND' | 'OR'));
                  resetEvidence();
                }}
              >
                <option value="AND">AND</option>
                <option value="OR">OR</option>
              </select>
            </label>
            <button className="secondary compact-button" type="button" onClick={() => addRule(group.id)} disabled={ruleCount >= maxFilterRules}>
              Add filter
            </button>
            <button className="secondary compact-button" type="button" onClick={() => addGroup(group.id)} disabled={depth >= maxFilterGroupDepth}>
              Add group
            </button>
            {depthIndex > 1 ? (
              <button className="danger-ghost compact-button" type="button" onClick={() => removeGroup(group.id)}>Remove group</button>
            ) : null}
          </div>
        </div>

        <div className="filter-group-rules">
          {group.rules.length
            ? group.rules.map((rule) => renderRule(rule, group.id, country, state))
            : <div className="empty-state compact-empty"><strong>No direct rules</strong><p>Add a filter or a nested group.</p></div>}
        </div>

        {group.children.length ? (
          <div className="filter-group-children">
            {group.children.map((child) => renderGroup(child, depthIndex + 1, country, state))}
          </div>
        ) : null}
      </section>
    );
  }

  if (catalogue.state !== 'ready') {
    return (
      <section className="card" aria-labelledby="filter-heading">
        <h2 id="filter-heading">Cohort rules</h2>
        {catalogue.state === 'loading' ? (
          <p className="muted">{catalogue.message}</p>
        ) : (
          <>
            <div className="alert alert-danger" role="alert">
              <strong>Audience builder unavailable.</strong>
              <p>{catalogue.message}</p>
              <p>The console will not substitute client-side filter, geography, organisation or purpose definitions when governed catalogues cannot be loaded.</p>
            </div>
            <button className="secondary" type="button" onClick={retryCatalogue}>Retry authoritative catalogues</button>
          </>
        )}
      </section>
    );
  }

  const breakdown = estimate.value?.breakdown;

  return (
    <div className="workspace-stack">
      <div className="filter-layout">
      <section className="grid" aria-labelledby="filter-heading">
        <div className="card">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Governed cohort logic</span>
              <h2 id="filter-heading">Nested audience rules</h2>
              <p className="muted">Build AND/OR groups from the server-managed filter registry. Nested depth and rule limits mirror the control plane.</p>
            </div>
            <div className="button-row">
              <span className="pill">{ruleCount} / {maxFilterRules} rules</span>
              <span className="pill">{depth} / {maxFilterGroupDepth} levels</span>
            </div>
          </div>
          {geographyError ? <div className="alert alert-danger" role="alert">{geographyError}</div> : null}
        </div>

        {renderGroup(root, 1)}

        <div className="button-row">
          <button
            className="primary"
            type="button"
            onClick={validate}
            disabled={validation.state === 'loading' || ruleCount === 0}
          >
            {validation.state === 'loading' ? 'Validating…' : 'Validate cohort'}
          </button>
        </div>
        {validation.message ? (
          <div
            className={`alert ${validation.state === 'valid' ? 'alert-success' : validation.state === 'invalid' ? 'alert-danger' : ''}`}
            role={validation.state === 'invalid' ? 'alert' : 'status'}
          >
            {validation.message}
          </div>
        ) : null}
      </section>

      <aside className="card sticky-panel" aria-label="Audience estimate and segment management">
        <h2>Authoritative eligibility</h2>
        <p className="muted">
          Counts come from one server-side eligibility query: matching profiles → current consent → suppression → frequency-cap enforcement.
        </p>

        <div className="form-stack">
          <label>Organisation
            <select
              value={organisationId}
              onChange={(event) => {
                setOrganisationId(event.target.value);
                setPurposes([]);
                setPurposeId('');
                setLoadedSegmentId('');
                resetEvidence();
              }}
            >
              <option value="">Select organisation</option>
              {organisations.map((item) => (
                <option value={item.id} key={item.id}>{item.tradingName || item.legalName}</option>
              ))}
            </select>
          </label>
          <label>Consent purpose
            <select
              value={purposeId}
              disabled={!organisationId}
              onChange={(event) => {
                setPurposeId(event.target.value);
                resetEvidence();
              }}
            >
              <option value="">Select active WhatsApp purpose</option>
              {purposes.map((item) => <option value={item.id} key={item.id}>{item.code} — {item.name}</option>)}
            </select>
          </label>
          {selectedPurpose ? <small>{selectedPurpose.description || selectedPurpose.name} · wording {selectedPurpose.wordingVersion}</small> : null}
          <label>Channel<input value="WHATSAPP" readOnly aria-readonly="true" /></label>
          <button
            className="secondary"
            type="button"
            onClick={estimateAudience}
            disabled={
              validation.state !== 'valid' ||
              (estimate.state === 'loading' || estimate.state === 'pending') ||
              !organisationId ||
              !purposeId ||
              !definitionPayload
            }
          >
            {estimate.state === 'loading'
              ? 'Scheduling…'
              : estimate.state === 'pending'
                ? 'Estimating…'
                : 'Estimate governed audience'}
          </button>
        </div>

        {breakdown ? (
          <ol className="eligibility-waterfall" aria-label="Eligibility waterfall">
            <li><span>Matching active profiles</span><strong>{breakdown.matchedProfiles.toLocaleString()}</strong></li>
            <li><span>Excluded — no current consent</span><strong>−{breakdown.consentExcluded.toLocaleString()}</strong></li>
            <li className="waterfall-stage"><span>Consent-eligible</span><strong>{breakdown.consentEligible.toLocaleString()}</strong></li>
            <li><span>Excluded — suppression</span><strong>−{breakdown.suppressionExcluded.toLocaleString()}</strong></li>
            <li className="waterfall-stage"><span>Unsuppressed</span><strong>{breakdown.unsuppressed.toLocaleString()}</strong></li>
            <li><span>Excluded — frequency cap</span><strong>−{breakdown.frequencyCapExcluded.toLocaleString()}</strong></li>
            <li className="waterfall-final"><span>Final eligible</span><strong>{breakdown.eligible.toLocaleString()}</strong></li>
          </ol>
        ) : (
          <ul className="summary-list">
            <li><span>Final eligible</span><strong>{estimate.value ? estimate.value.eligibleCount.toLocaleString() : '—'}</strong></li>
            <li><span>Eligibility waterfall</span><strong>{estimate.value ? 'Not reported by backend' : 'Not run'}</strong></li>
          </ul>
        )}
        <div className="summary-list">
          <div>
            <span>As of</span>
            <strong>{estimate.value?.asOf ? new Date(estimate.value.asOf).toLocaleString() : estimate.value ? 'Not reported by backend' : 'Not run'}</strong>
          </div>
          <div>
            <span>Calculated</span>
            <strong>{estimate.value ? new Date(estimate.value.calculatedAt).toLocaleString() : 'Not run'}</strong>
          </div>
        </div>

        {estimate.message ? (
          <div className={`alert ${estimate.state === 'error' ? 'alert-danger' : estimate.state === 'ready' ? 'alert-success' : ''}`} role={estimate.state === 'error' ? 'alert' : 'status'}>
            {estimate.message}
          </div>
        ) : null}

        <hr className="panel-divider" />

        <h3>Save reusable segment</h3>
        <p className="muted">The saved segment preserves this nested server-validated definition and can later be versioned, cloned and frozen into a campaign snapshot.</p>
        <div className="form-stack">
          <label>Segment name
            <input
              value={segmentName}
              maxLength={160}
              onChange={(event) => {
                setSegmentName(event.target.value);
                saveRequest.current += 1;
                setSaveState({ state: 'idle', message: '' });
              }}
              placeholder="e.g. Lagos adults or Gold customers"
            />
          </label>
          <label>Description
            <textarea
              value={segmentDescription}
              maxLength={2000}
              rows={3}
              onChange={(event) => {
                setSegmentDescription(event.target.value);
                saveRequest.current += 1;
                setSaveState({ state: 'idle', message: '' });
              }}
              placeholder="Optional internal description"
            />
          </label>
          <button
            className="primary"
            type="button"
            onClick={saveSegment}
            disabled={
              validation.state !== 'valid' ||
              saveState.state === 'saving' ||
              !organisationId ||
              !segmentName.trim() ||
              !definitionPayload
            }
          >
            {saveState.state === 'saving' ? 'Saving…' : 'Save governed segment'}
          </button>
        </div>
        {saveState.message ? (
          <div className={`alert ${saveState.state === 'error' ? 'alert-danger' : saveState.state === 'saved' ? 'alert-success' : ''}`} role={saveState.state === 'error' ? 'alert' : 'status'}>
            {saveState.message}
          </div>
        ) : null}

        <h3>Human-readable definition</h3>
        <p>{summary || 'No filters configured.'}</p>
        <details>
          <summary>Technical definition</summary>
          <pre>{definitionPayload ? JSON.stringify(definitionPayload, null, 2) : 'Definition is not valid yet.'}</pre>
        </details>
      </aside>
      </div>

      <SegmentSnapshotManager
        organisationId={organisationId}
        currentDefinition={definitionPayload}
        completedEstimateId={estimate.state === 'ready' ? estimate.job?.id : undefined}
        loadedSegmentId={loadedSegmentId}
        editorName={segmentName}
        editorDescription={segmentDescription}
        refreshToken={segmentRefreshToken}
        onOpenSegment={openSavedSegment}
      />
    </div>
  );
}
