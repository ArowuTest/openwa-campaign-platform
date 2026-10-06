import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildCreateSegmentRequest,
  buildEstimateRequest,
  buildFilterGroup,
  createRule,
  requireAuthoritativeCatalogue
} from '../lib/audience-builder-model.ts';

const serverDefinitions = [
  {
    code: 'COUNTRY',
    displayName: 'Country',
    description: 'Server country',
    dataType: 'geography',
    operators: ['in', 'not_in', 'is_known', 'is_unknown'],
    allowedValues: []
  },
  {
    code: 'REPORTED_AGE',
    displayName: 'Reported age',
    description: 'Server age',
    dataType: 'integer',
    operators: ['equals', 'between', 'greater_than', 'less_than', 'is_known', 'is_unknown'],
    allowedValues: [],
    sensitive: true
  },
  {
    code: 'CUSTOMER_TIER',
    displayName: 'Customer tier',
    description: 'Dynamic server-managed dimension',
    dataType: 'single_select',
    operators: ['in', 'not_in'],
    allowedValues: ['Gold', 'Silver']
  }
];

test('audience builder fails closed when authoritative catalogues are unavailable or empty', () => {
  assert.throws(() => requireAuthoritativeCatalogue([], []), /filter catalogue.*empty/i);
  assert.throws(() => requireAuthoritativeCatalogue(serverDefinitions, []), /geography catalogue.*empty/i);
  assert.throws(
    () => requireAuthoritativeCatalogue([{ code: 'COUNTRY', displayName: 'Country', dataType: 'geography', operators: [] }], [{ iso2: 'NG', name: 'Nigeria' }]),
    /invalid definition/i
  );
});

test('audience builder preserves server-defined operators and dynamic allowed values', () => {
  const result = requireAuthoritativeCatalogue(serverDefinitions, [{ iso2: 'NG', name: 'Nigeria' }]);
  assert.deepEqual(result.definitions[0].operators, ['in', 'not_in', 'is_known', 'is_unknown']);
  assert.deepEqual(result.definitions[2].allowedValues, ['Gold', 'Silver']);
});

test('filter payload is built only from authoritative definitions and coerces governed types', () => {
  const rules = [
    { ...createRule(serverDefinitions[0], 1), values: ['NG'] },
    { id: 2, definitionCode: 'REPORTED_AGE', operator: 'between', values: ['18', '35'] },
    { id: 3, definitionCode: 'CUSTOMER_TIER', operator: 'in', values: ['Gold'] }
  ];
  assert.deepEqual(buildFilterGroup(rules, serverDefinitions), {
    join: 'AND',
    rules: [
      { definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] },
      { definitionCode: 'REPORTED_AGE', operator: 'between', values: [18, 35] },
      { definitionCode: 'CUSTOMER_TIER', operator: 'in', values: ['Gold'] }
    ]
  });

  assert.throws(
    () => buildFilterGroup([{ id: 1, definitionCode: 'CLIENT_ONLY', operator: 'in', values: ['x'] }], serverDefinitions),
    /no longer authoritative/i
  );
});

test('cohort estimate request requires governed organisation and purpose context', () => {
  const definition = buildFilterGroup([{ ...createRule(serverDefinitions[0], 1), values: ['NG'] }], serverDefinitions);
  assert.deepEqual(
    buildEstimateRequest(definition, { organisationId: 'org-1', purposeId: 'purpose-1', channel: 'WHATSAPP' }),
    {
      definition,
      organisationId: 'org-1',
      purposeId: 'purpose-1',
      channel: 'WHATSAPP'
    }
  );
  assert.throws(
    () => buildEstimateRequest(definition, { organisationId: '', purposeId: 'purpose-1', channel: 'WHATSAPP' }),
    /Organisation ID is required/i
  );
});

test('saved segment request is governed by organisation, name and current definition', () => {
  const definition = buildFilterGroup([{ ...createRule(serverDefinitions[0], 1), values: ['NG'] }], serverDefinitions);
  assert.deepEqual(
    buildCreateSegmentRequest(definition, {
      organisationId: 'org-1',
      name: ' Lagos adults ',
      description: ' Governed reusable cohort '
    }),
    {
      organisationId: 'org-1',
      name: 'Lagos adults',
      description: 'Governed reusable cohort',
      definition
    }
  );
  assert.throws(
    () => buildCreateSegmentRequest(definition, { organisationId: 'org-1', name: '' }),
    /Segment name is required/i
  );
});
