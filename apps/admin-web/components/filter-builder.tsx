'use client';

import { useEffect, useMemo, useState } from 'react';

import { apiRequest } from '../lib/api';
import {
  buildCreateSegmentRequest,
  buildEstimateRequest,
  buildFilterGroup,
  createRule,
  requireAuthoritativeCatalogue,
  type CohortEstimate,
  type FilterDefinition,
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

type EstimateState = {
  state: 'idle' | 'loading' | 'ready' | 'error';
  message: string;
  value?: CohortEstimate;
};

type SaveState = {
  state: 'idle' | 'saving' | 'saved' | 'error';
  message: string;
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

export function FilterBuilder() {
  const [definitions, setDefinitions] = useState<FilterDefinition[]>([]);
  const [rules, setRules] = useState<Rule[]>([]);
  const [countries, setCountries] = useState<GeographyItem[]>([]);
  const [stateOptions, setStateOptions] = useState<{ country: string; items: GeographyItem[] }>({ country: '', items: [] });
  const [lgaOptions, setLGAOptions] = useState<{ country: string; state: string; items: GeographyItem[] }>({ country: '', state: '', items: [] });
  const [catalogue, setCatalogue] = useState<CatalogueState>({ state: 'loading', message: 'Loading the authoritative filter and geography catalogues…' });
  const [catalogueAttempt, setCatalogueAttempt] = useState(0);
  const [geographyError, setGeographyError] = useState('');
  const [validation, setValidation] = useState<ValidationState>({ state: 'idle', message: '' });
  const [estimate, setEstimate] = useState<EstimateState>({ state: 'idle', message: '' });
  const [organisationId, setOrganisationId] = useState('');
  const [purposeId, setPurposeId] = useState('');
  const [segmentName, setSegmentName] = useState('');
  const [segmentDescription, setSegmentDescription] = useState('');
  const [saveState, setSaveState] = useState<SaveState>({ state: 'idle', message: '' });

  const selectedCountry = rules.find((rule) => rule.definitionCode === 'COUNTRY')?.values[0] ?? '';
  const selectedState = rules.find((rule) => rule.definitionCode === 'STATE')?.values[0] ?? '';

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      apiRequest<{ items: FilterDefinition[] }>('/v1/filter-definitions'),
      apiRequest<{ items: GeographyItem[] }>('/v1/geography/countries')
    ]).then(([definitionPayload, countryPayload]) => {
      if (cancelled) return;
      const authoritative = requireAuthoritativeCatalogue(definitionPayload.items, countryPayload.items);
      setDefinitions(authoritative.definitions);
      setCountries(authoritative.countries);
      setRules([createRule(authoritative.definitions[0], 1)]);
      setCatalogue({ state: 'ready', message: '' });
      setGeographyError('');
      setValidation({ state: 'idle', message: '' });
      setEstimate({ state: 'idle', message: '' });
    }).catch((cause) => {
      if (cancelled) return;
      setDefinitions([]);
      setCountries([]);
      setRules([]);
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
    if (!selectedCountry) return;
    let cancelled = false;
    void apiRequest<{ items: GeographyItem[] }>(
      `/v1/geography/areas?country=${encodeURIComponent(selectedCountry)}&level=1`
    ).then((payload) => {
      if (cancelled) return;
      setStateOptions({ country: selectedCountry, items: payload.items ?? [] });
      setGeographyError('');
    }).catch((cause) => {
      if (cancelled) return;
      setStateOptions({ country: selectedCountry, items: [] });
      setGeographyError(cause instanceof Error ? cause.message : 'State/region catalogue could not be loaded.');
    });
    return () => { cancelled = true; };
  }, [selectedCountry]);

  useEffect(() => {
    if (!selectedCountry || !selectedState) return;
    let cancelled = false;
    void apiRequest<{ items: GeographyItem[] }>(
      `/v1/geography/areas?country=${encodeURIComponent(selectedCountry)}&parent=${encodeURIComponent(selectedState)}&level=2`
    ).then((payload) => {
      if (cancelled) return;
      setLGAOptions({ country: selectedCountry, state: selectedState, items: payload.items ?? [] });
      setGeographyError('');
    }).catch((cause) => {
      if (cancelled) return;
      setLGAOptions({ country: selectedCountry, state: selectedState, items: [] });
      setGeographyError(cause instanceof Error ? cause.message : 'LGA/district catalogue could not be loaded.');
    });
    return () => { cancelled = true; };
  }, [selectedCountry, selectedState]);

  const definitionPayload = useMemo(() => {
    try {
      return buildFilterGroup(rules, definitions);
    } catch {
      return undefined;
    }
  }, [definitions, rules]);

  const summary = useMemo(() => rules.map((rule) => {
    const definition = definitions.find((item) => item.code === rule.definitionCode);
    return `${definition?.displayName ?? rule.definitionCode} ${rule.operator.replaceAll('_', ' ')} ${rule.values.join(' and ')}`;
  }).join(' AND '), [definitions, rules]);

  function resetEvidence() {
    setValidation({ state: 'idle', message: '' });
    setEstimate({ state: 'idle', message: '' });
    setSaveState({ state: 'idle', message: '' });
  }

  function retryCatalogue() {
    setDefinitions([]);
    setCountries([]);
    setRules([]);
    setCatalogue({ state: 'loading', message: 'Reloading the authoritative filter and geography catalogues…' });
    setGeographyError('');
    resetEvidence();
    setCatalogueAttempt((value) => value + 1);
  }

  function updateRule(id: number, patch: Partial<Rule>) {
    setRules((current) => current.map((rule) => rule.id === id ? { ...rule, ...patch } : rule));
    setGeographyError('');
    resetEvidence();
  }

  function addRule() {
    const definition = definitions.find((item) => !rules.some((rule) => rule.definitionCode === item.code)) ?? definitions[0];
    if (!definition) return;
    const id = Math.max(0, ...rules.map((rule) => rule.id)) + 1;
    setRules((current) => [...current, createRule(definition, id)]);
    resetEvidence();
  }

  async function validate() {
    let authoritative;
    try {
      authoritative = buildFilterGroup(rules, definitions);
    } catch (cause) {
      setValidation({ state: 'invalid', message: cause instanceof Error ? cause.message : 'Cohort definition is invalid.' });
      return;
    }
    setValidation({ state: 'loading', message: 'Validating cohort definition against the authoritative registry…' });
    setEstimate({ state: 'idle', message: '' });
    try {
      await apiRequest('/v1/cohorts/validate', {
        method: 'POST',
        body: JSON.stringify(authoritative)
      });
      setValidation({
        state: 'valid',
        message: 'Cohort definition is valid for the current governed filter registry and operator permissions.'
      });
    } catch (cause) {
      setValidation({
        state: 'invalid',
        message: cause instanceof Error ? cause.message : 'Cohort definition is invalid.'
      });
    }
  }

  async function estimateAudience() {
    let definition;
    try {
      definition = buildFilterGroup(rules, definitions);
      const request = buildEstimateRequest(definition, {
        organisationId,
        purposeId,
        channel: 'WHATSAPP'
      });
      setEstimate({ state: 'loading', message: 'Calculating authoritative eligible audience…' });
      const value = await apiRequest<CohortEstimate>('/v1/cohorts/estimate', {
        method: 'POST',
        body: JSON.stringify(request)
      });
      setEstimate({
        state: 'ready',
        value,
        message: 'Authoritative final eligible count returned by the control plane.'
      });
    } catch (cause) {
      setEstimate({
        state: 'error',
        message: cause instanceof Error ? cause.message : 'Audience estimate could not be calculated.'
      });
    }
  }

  async function saveSegment() {
    try {
      const definition = buildFilterGroup(rules, definitions);
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
      setSaveState({
        state: 'saved',
        message: `Saved ${saved.name} as segment ${saved.id} · version ${saved.version}.`
      });
    } catch (cause) {
      setSaveState({
        state: 'error',
        message: cause instanceof Error ? cause.message : 'Segment could not be saved.'
      });
    }
  }

  function optionsFor(definition: FilterDefinition): GeographyItem[] | string[] {
    if (definition.code === 'COUNTRY') return countries;
    if (definition.code === 'STATE') {
      return stateOptions.country === selectedCountry ? stateOptions.items : [];
    }
    if (definition.code === 'LGA') {
      return lgaOptions.country === selectedCountry && lgaOptions.state === selectedState
        ? lgaOptions.items
        : [];
    }
    return definition.allowedValues ?? [];
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
              <p>The console will not substitute client-side filter or geography definitions when the governed catalogue cannot be loaded.</p>
            </div>
            <button className="secondary" type="button" onClick={retryCatalogue}>Retry authoritative catalogue</button>
          </>
        )}
      </section>
    );
  }

  return (
    <div className="filter-layout">
      <section className="grid" aria-labelledby="filter-heading">
        <div className="card">
          <div className="section-heading">
            <div>
              <h2 id="filter-heading">Cohort rules</h2>
              <p className="muted">Fields, operators and governed allowed values come from the current server-side filter registry.</p>
            </div>
            <span className="pill">AND group</span>
          </div>
          {geographyError ? <div className="alert alert-danger" role="alert">{geographyError}</div> : null}
        </div>

        {rules.map((rule) => {
          const definition = definitions.find((item) => item.code === rule.definitionCode);
          if (!definition) {
            return (
              <div className="alert alert-danger" role="alert" key={rule.id}>
                Filter definition {rule.definitionCode} is no longer available. Reload the authoritative catalogue.
              </div>
            );
          }
          const options = optionsFor(definition);
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
                  updateRule(rule.id, {
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
                <select value={rule.operator} onChange={(event) => updateRule(rule.id, {
                  operator: event.target.value,
                  values: initialValues(event.target.value)
                })}>
                  {definition.operators.map((operator) => <option value={operator} key={operator}>{operator.replaceAll('_', ' ')}</option>)}
                </select>
              </label>

              <div>
                <span className="field-label">Value</span>
                {isNoValue ? <div className="value-placeholder">No value required</div> : options.length > 0 ? (
                  <select value={rule.values[0] ?? ''} onChange={(event) => updateRule(rule.id, { values: [event.target.value] })}>
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
                      onChange={(event) => updateRule(rule.id, { values: [event.target.value, rule.values[1] ?? ''] })}
                      aria-label={`${definition.displayName} lower bound`}
                    />
                    <span>to</span>
                    <input
                      type={valueType}
                      step={definition.dataType === 'decimal' ? 'any' : undefined}
                      min={definition.dataType === 'integer' ? 0 : undefined}
                      max={definition.code === 'REPORTED_AGE' ? 130 : undefined}
                      value={rule.values[1] ?? ''}
                      onChange={(event) => updateRule(rule.id, { values: [rule.values[0] ?? '', event.target.value] })}
                      aria-label={`${definition.displayName} upper bound`}
                    />
                  </div>
                ) : definition.dataType === 'boolean' ? (
                  <select value={rule.values[0] ?? ''} onChange={(event) => updateRule(rule.id, { values: [event.target.value] })}>
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
                    onChange={(event) => updateRule(rule.id, { values: [event.target.value] })}
                    placeholder="Enter a value"
                  />
                )}
              </div>

              <button
                className="danger-ghost"
                type="button"
                onClick={() => {
                  setRules((current) => current.filter((item) => item.id !== rule.id));
                  resetEvidence();
                }}
                aria-label={`Remove ${definition.displayName} filter`}
              >
                Remove
              </button>
            </div>
          );
        })}

        <div className="button-row">
          <button className="secondary" type="button" onClick={addRule}>Add filter</button>
          <button
            className="primary"
            type="button"
            onClick={validate}
            disabled={validation.state === 'loading' || rules.length === 0}
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

      <aside className="card sticky-panel" aria-label="Audience estimate">
        <h2>Authoritative audience estimate</h2>
        <p className="muted">
          The control plane returns the final eligible count after governed cohort eligibility. It does not currently return a reconciled
          profile → consent → suppression → cap waterfall, so the console does not infer those intermediate numbers.
        </p>

        <div className="form-stack">
          <label>
            Organisation ID
            <input
              value={organisationId}
              onChange={(event) => {
                setOrganisationId(event.target.value);
                setEstimate({ state: 'idle', message: '' });
                setSaveState({ state: 'idle', message: '' });
              }}
              placeholder="Governed organisation ID"
            />
          </label>
          <label>
            Consent purpose ID
            <input
              value={purposeId}
              onChange={(event) => {
                setPurposeId(event.target.value);
                setEstimate({ state: 'idle', message: '' });
              }}
              placeholder="Governed purpose ID"
            />
          </label>
          <label>
            Channel
            <input value="WHATSAPP" readOnly aria-readonly="true" />
          </label>
          <button
            className="secondary"
            type="button"
            onClick={estimateAudience}
            disabled={
              validation.state !== 'valid' ||
              estimate.state === 'loading' ||
              !organisationId.trim() ||
              !purposeId.trim() ||
              !definitionPayload
            }
          >
            {estimate.state === 'loading' ? 'Estimating…' : 'Estimate final eligible'}
          </button>
        </div>

        <ul className="summary-list">
          <li>
            <span>Final eligible</span>
            <strong>{estimate.value ? estimate.value.eligibleCount.toLocaleString() : '—'}</strong>
          </li>
          <li>
            <span>Calculated</span>
            <strong>{estimate.value ? new Date(estimate.value.calculatedAt).toLocaleString() : 'Not run'}</strong>
          </li>
          <li>
            <span>Intermediate waterfall</span>
            <strong>Not reported</strong>
          </li>
        </ul>

        {estimate.message ? (
          <div className={`alert ${estimate.state === 'error' ? 'alert-danger' : estimate.state === 'ready' ? 'alert-success' : ''}`} role={estimate.state === 'error' ? 'alert' : 'status'}>
            {estimate.message}
          </div>
        ) : null}

        <hr className="panel-divider" />

        <h3>Save reusable segment</h3>
        <p className="muted">Saved segments are server-validated against the same current filter registry and operator permissions.</p>
        <div className="form-stack">
          <label>
            Segment name
            <input
              value={segmentName}
              maxLength={160}
              onChange={(event) => {
                setSegmentName(event.target.value);
                setSaveState({ state: 'idle', message: '' });
              }}
              placeholder="e.g. Lagos active customers"
            />
          </label>
          <label>
            Description
            <textarea
              value={segmentDescription}
              maxLength={2000}
              rows={3}
              onChange={(event) => {
                setSegmentDescription(event.target.value);
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
              !organisationId.trim() ||
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
  );
}
