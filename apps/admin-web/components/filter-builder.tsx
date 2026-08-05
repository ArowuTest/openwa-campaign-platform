'use client';

import { useEffect, useMemo, useState } from 'react';

type FilterDefinition = {
  code: string;
  displayName: string;
  description: string;
  dataType: string;
  operators: string[];
  sensitive?: boolean;
};

type GeographyItem = {
  iso2?: string;
  code?: string;
  name: string;
  parentCode?: string;
};

type Rule = {
  id: number;
  definitionCode: string;
  operator: string;
  values: string[];
};

const fallbackDefinitions: FilterDefinition[] = [
  { code: 'COUNTRY', displayName: 'Country', description: 'Country', dataType: 'geography', operators: ['in', 'not_in'] },
  { code: 'STATE', displayName: 'State or region', description: 'State or region', dataType: 'geography', operators: ['in', 'not_in'] },
  { code: 'LGA', displayName: 'LGA or district', description: 'LGA or district', dataType: 'geography', operators: ['in', 'not_in'] },
  { code: 'REPORTED_AGE', displayName: 'Reported age', description: 'Self-declared age', dataType: 'integer', operators: ['between', 'greater_than', 'less_than'] },
  { code: 'GENDER', displayName: 'Gender', description: 'Self-declared gender', dataType: 'single_select', operators: ['in', 'not_in'] }
];

const initialRules: Rule[] = [
  { id: 1, definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] },
  { id: 2, definitionCode: 'STATE', operator: 'in', values: ['LAGOS'] },
  { id: 3, definitionCode: 'REPORTED_AGE', operator: 'between', values: ['18', '35'] }
];

export function FilterBuilder() {
  const [definitions, setDefinitions] = useState<FilterDefinition[]>(fallbackDefinitions);
  const [rules, setRules] = useState<Rule[]>(initialRules);
  const [countries, setCountries] = useState<GeographyItem[]>([]);
  const [states, setStates] = useState<GeographyItem[]>([]);
  const [lgas, setLGAs] = useState<GeographyItem[]>([]);
  const [validation, setValidation] = useState<{ state: 'idle' | 'loading' | 'valid' | 'invalid'; message: string }>({ state: 'idle', message: '' });

  const selectedCountry = rules.find((rule) => rule.definitionCode === 'COUNTRY')?.values[0] ?? '';
  const selectedState = rules.find((rule) => rule.definitionCode === 'STATE')?.values[0] ?? '';

  useEffect(() => {
    Promise.all([
      fetch('/api/v1/filter-definitions').then((response) => response.ok ? response.json() : Promise.reject()),
      fetch('/api/v1/geography/countries').then((response) => response.ok ? response.json() : Promise.reject())
    ]).then(([definitionPayload, countryPayload]) => {
      setDefinitions(definitionPayload.items ?? fallbackDefinitions);
      setCountries(countryPayload.items ?? []);
    }).catch(() => {
      setDefinitions(fallbackDefinitions);
      setCountries([{ iso2: 'NG', name: 'Nigeria' }, { iso2: 'GH', name: 'Ghana' }, { iso2: 'GB', name: 'United Kingdom' }]);
    });
  }, []);

  useEffect(() => {
    if (!selectedCountry) {
      setStates([]);
      return;
    }
    fetch(`/api/v1/geography/areas?country=${encodeURIComponent(selectedCountry)}&level=1`)
      .then((response) => response.ok ? response.json() : Promise.reject())
      .then((payload) => setStates(payload.items ?? []))
      .catch(() => setStates([]));
  }, [selectedCountry]);

  useEffect(() => {
    if (!selectedCountry || !selectedState) {
      setLGAs([]);
      return;
    }
    fetch(`/api/v1/geography/areas?country=${encodeURIComponent(selectedCountry)}&parent=${encodeURIComponent(selectedState)}&level=2`)
      .then((response) => response.ok ? response.json() : Promise.reject())
      .then((payload) => setLGAs(payload.items ?? []))
      .catch(() => setLGAs([]));
  }, [selectedCountry, selectedState]);

  const definitionPayload = useMemo(() => ({
    join: 'AND',
    rules: rules.map((rule) => ({
      definitionCode: rule.definitionCode,
      operator: rule.operator,
      values: rule.values.map((value) => {
        if (rule.definitionCode === 'REPORTED_AGE' && value !== '') return Number(value);
        return value;
      }).filter((value) => value !== '')
    }))
  }), [rules]);

  const summary = useMemo(() => rules.map((rule) => {
    const definition = definitions.find((item) => item.code === rule.definitionCode);
    return `${definition?.displayName ?? rule.definitionCode} ${rule.operator.replaceAll('_', ' ')} ${rule.values.join(' and ')}`;
  }).join(' AND '), [definitions, rules]);

  function updateRule(id: number, patch: Partial<Rule>) {
    setRules((current) => current.map((rule) => rule.id === id ? { ...rule, ...patch } : rule));
    setValidation({ state: 'idle', message: '' });
  }

  function addRule() {
    const id = Math.max(0, ...rules.map((rule) => rule.id)) + 1;
    const defaultDefinition = definitions.find((item) => !rules.some((rule) => rule.definitionCode === item.code)) ?? definitions[0];
    setRules((current) => [...current, {
      id,
      definitionCode: defaultDefinition.code,
      operator: defaultDefinition.operators[0],
      values: defaultDefinition.operators[0] === 'between' ? ['', ''] : ['']
    }]);
  }

  async function validate() {
    setValidation({ state: 'loading', message: 'Validating cohort definition…' });
    const response = await fetch('/api/v1/cohorts/validate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(definitionPayload)
    });
    const result = await response.json().catch(() => ({}));
    if (response.ok) setValidation({ state: 'valid', message: 'Cohort definition is structurally valid. Consent and suppression checks will be applied automatically.' });
    else setValidation({ state: 'invalid', message: result.message ?? result.details?.detail ?? 'Cohort definition is invalid.' });
  }

  function optionsFor(code: string): GeographyItem[] | string[] {
    if (code === 'COUNTRY') return countries;
    if (code === 'STATE') return states;
    if (code === 'LGA') return lgas;
    if (code === 'GENDER') return ['FEMALE', 'MALE', 'OTHER', 'PREFER_NOT_TO_SAY', 'NOT_STATED'];
    return [];
  }

  return (
    <div className="filter-layout">
      <section className="grid" aria-labelledby="filter-heading">
        <div className="card">
          <div className="section-heading">
            <div>
              <h2 id="filter-heading">Cohort rules</h2>
              <p className="muted">Filters are registry-driven. Newly approved dimensions can appear without rebuilding this page.</p>
            </div>
            <span className="pill">AND group</span>
          </div>
        </div>

        {rules.map((rule) => {
          const definition = definitions.find((item) => item.code === rule.definitionCode) ?? definitions[0];
          const options = optionsFor(rule.definitionCode);
          const isBetween = rule.operator === 'between';
          const isNoValue = rule.operator === 'is_known' || rule.operator === 'is_unknown';
          return (
            <div className="rule" key={rule.id}>
              <label>
                <span className="field-label">Field</span>
                <select value={rule.definitionCode} onChange={(event) => {
                  const next = definitions.find((item) => item.code === event.target.value) ?? definitions[0];
                  updateRule(rule.id, {
                    definitionCode: next.code,
                    operator: next.operators[0],
                    values: next.operators[0] === 'between' ? ['', ''] : ['']
                  });
                }}>
                  {definitions.map((item) => <option value={item.code} key={item.code}>{item.displayName}</option>)}
                </select>
                {definition.sensitive ? <small className="sensitive-label">Sensitive demographic field</small> : null}
              </label>

              <label>
                <span className="field-label">Operator</span>
                <select value={rule.operator} onChange={(event) => updateRule(rule.id, {
                  operator: event.target.value,
                  values: event.target.value === 'between' ? ['', ''] : event.target.value.startsWith('is_') ? [] : ['']
                })}>
                  {definition.operators.map((operator) => <option value={operator} key={operator}>{operator.replaceAll('_', ' ')}</option>)}
                </select>
              </label>

              <div>
                <span className="field-label">Value</span>
                {isNoValue ? <div className="value-placeholder">No value required</div> : options.length > 0 ? (
                  <select value={rule.values[0] ?? ''} onChange={(event) => updateRule(rule.id, { values: [event.target.value] })}>
                    <option value="">Select a value</option>
                    {options.map((option) => typeof option === 'string'
                      ? <option value={option} key={option}>{option.replaceAll('_', ' ')}</option>
                      : <option value={option.iso2 ?? option.code} key={option.iso2 ?? option.code}>{option.name}</option>
                    )}
                  </select>
                ) : isBetween ? (
                  <div className="range-inputs">
                    <input type="number" min={0} max={130} value={rule.values[0] ?? ''} onChange={(event) => updateRule(rule.id, { values: [event.target.value, rule.values[1] ?? ''] })} aria-label={`${definition.displayName} lower bound`} />
                    <span>to</span>
                    <input type="number" min={0} max={130} value={rule.values[1] ?? ''} onChange={(event) => updateRule(rule.id, { values: [rule.values[0] ?? '', event.target.value] })} aria-label={`${definition.displayName} upper bound`} />
                  </div>
                ) : (
                  <input value={rule.values[0] ?? ''} onChange={(event) => updateRule(rule.id, { values: [event.target.value] })} placeholder="Enter a value" />
                )}
              </div>

              <button className="danger-ghost" type="button" onClick={() => setRules((current) => current.filter((item) => item.id !== rule.id))} aria-label={`Remove ${definition.displayName} filter`}>Remove</button>
            </div>
          );
        })}

        <div className="button-row">
          <button className="secondary" type="button" onClick={addRule}>Add filter</button>
          <button className="primary" type="button" onClick={validate} disabled={validation.state === 'loading'}>{validation.state === 'loading' ? 'Validating…' : 'Validate cohort'}</button>
        </div>
        {validation.message ? <div className={`alert ${validation.state === 'valid' ? 'alert-success' : validation.state === 'invalid' ? 'alert-danger' : ''}`}>{validation.message}</div> : null}
      </section>

      <aside className="card sticky-panel" aria-label="Audience estimate">
        <h2>Audience estimate</h2>
        <p className="muted">Live database counts will be enabled after PostgreSQL repository integration. Eligibility will always include consent and suppression controls.</p>
        <ul className="summary-list">
          <li><span>Profile matches</span><strong>—</strong></li>
          <li><span>Active relevant consent</span><strong>—</strong></li>
          <li><span>Suppressed</span><strong>—</strong></li>
          <li><span>Final eligible</span><strong>—</strong></li>
        </ul>
        <h3>Human-readable definition</h3>
        <p>{summary || 'No filters configured.'}</p>
        <details>
          <summary>Technical definition</summary>
          <pre>{JSON.stringify(definitionPayload, null, 2)}</pre>
        </details>
      </aside>
    </div>
  );
}
