import assert from 'node:assert/strict';
import test from 'node:test';
import * as audienceBuilder from '../lib/audience-builder-model.ts';

import {
  buildCreateSegmentRequest,
  buildEstimateRequest,
  addChildGroup,
  addRuleToGroup,
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
  updateRuleInGroup
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

test('nested cohort tree preserves AND/OR structure and authoritative rule coercion', () => {
  let root = createFilterGroupNode(100, 'OR');
  root = addRuleToGroup(root, root.id, { ...createRule(serverDefinitions[0], 1), values: ['NG'] });

  let child = createFilterGroupNode(101, 'AND');
  child = addRuleToGroup(child, child.id, {
    id: 2,
    definitionCode: 'REPORTED_AGE',
    operator: 'between',
    values: ['18', '35']
  });
  child = addRuleToGroup(child, child.id, {
    id: 3,
    definitionCode: 'CUSTOMER_TIER',
    operator: 'in',
    values: ['Gold']
  });
  root = addChildGroup(root, root.id, child);

  assert.equal(countFilterRules(root), 3);
  assert.equal(filterGroupDepth(root), 2);
  assert.deepEqual(buildFilterGroup(root, serverDefinitions), {
    join: 'OR',
    rules: [{ definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] }],
    children: [{
      join: 'AND',
      rules: [
        { definitionCode: 'REPORTED_AGE', operator: 'between', values: [18, 35] },
        { definitionCode: 'CUSTOMER_TIER', operator: 'in', values: ['Gold'] }
      ]
    }]
  });

  root = updateGroupJoin(root, child.id, 'OR');
  root = updateRuleInGroup(root, child.id, 3, (rule) => ({ ...rule, values: ['Silver'] }));
  const updated = buildFilterGroup(root, serverDefinitions);
  assert.equal(updated.children?.[0]?.join, 'OR');
  assert.deepEqual(updated.children?.[0]?.rules?.[1]?.values, ['Silver']);

  root = removeRuleFromGroup(root, child.id, 2);
  assert.equal(countFilterRules(root), 2);
  root = removeChildGroup(root, child.id);
  assert.equal(countFilterRules(root), 1);
});

test('saved nested definition hydrates into an editable tree without semantic drift', () => {
  const definition = {
    join: 'OR' as const,
    rules: [{ definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] }],
    children: [{
      join: 'AND' as const,
      rules: [
        { definitionCode: 'REPORTED_AGE', operator: 'between', values: [18, 35] },
        { definitionCode: 'CUSTOMER_TIER', operator: 'in', values: ['Gold'] }
      ]
    }]
  };
  const hydrated = hydrateEditableFilterGroup(definition, 50);
  assert.equal(hydrated.group.id, 50);
  assert.ok(hydrated.nextId > 50);
  assert.deepEqual(buildFilterGroup(hydrated.group, serverDefinitions), definition);
});

// Go filter.Group uses omitempty for both rules and children.
const goGroupOnlyDefinition = JSON.parse('{"join":"OR","children":[{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"in","values":["NG"]},{"definitionCode":"REPORTED_AGE","operator":"between","values":[18,35]}]},{"join":"OR","children":[{"join":"AND","rules":[{"definitionCode":"CUSTOMER_TIER","operator":"in","values":["Gold","Silver"]}]}]}]}');

test('Go group-only JSON hydrates and rebuilds without losing joins or ordered values', () => {
  const hydrated = hydrateEditableFilterGroup(goGroupOnlyDefinition, 50);
  assert.equal(hydrated.group.id, 50);
  assert.deepEqual(hydrated.group.rules, []);
  assert.deepEqual(hydrated.group.children[1].rules, []);
  const rebuilt = buildFilterGroup(hydrated.group, serverDefinitions);
  assert.deepEqual(rebuilt, {
    join: 'OR',
    rules: [],
    children: [
      {
        join: 'AND',
        rules: [
          { definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] },
          { definitionCode: 'REPORTED_AGE', operator: 'between', values: [18, 35] }
        ]
      },
      {
        join: 'OR',
        rules: [],
        children: [{
          join: 'AND',
          rules: [{ definitionCode: 'CUSTOMER_TIER', operator: 'in', values: ['Gold', 'Silver'] }]
        }]
      }
    ]
  });
  assert.deepEqual(
    audienceBuilder.normalizeFilterGroup(rebuilt),
    audienceBuilder.normalizeFilterGroup(goGroupOnlyDefinition)
  );
  assert.equal(audienceBuilder.filterGroupsEqual(rebuilt, goGroupOnlyDefinition), true);
});

test('human-readable summary accepts Go group-only JSON without treating omitted rules as a rule', () => {
  assert.equal(
    audienceBuilder.humanizeFilterGroup(goGroupOnlyDefinition, serverDefinitions),
    '(Country in NG AND Reported age between 18 and 35) OR ((Customer tier in Gold and Silver))'
  );
});

// Go's nil []any values serialize as null; omitted input values also decode to nil.
const noValueWireCases = [
  {
    name: 'null',
    definition: JSON.parse('{"join":"OR","children":[{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known","values":null},{"definitionCode":"REPORTED_AGE","operator":"is_unknown","values":null}]}]}')
  },
  {
    name: 'omitted',
    definition: JSON.parse('{"join":"OR","children":[{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"is_known"},{"definitionCode":"REPORTED_AGE","operator":"is_unknown"}]}]}')
  }
];

for (const wire of noValueWireCases) {
  test(`Go no-value rules with ${wire.name} values hydrate and rebuild as empty arrays`, () => {
    const hydrated = hydrateEditableFilterGroup(wire.definition, 50);
    assert.deepEqual(hydrated.group.children[0].rules, [
      { id: 52, definitionCode: 'COUNTRY', operator: 'is_known', values: [] },
      { id: 53, definitionCode: 'REPORTED_AGE', operator: 'is_unknown', values: [] }
    ]);
    assert.deepEqual(buildFilterGroup(hydrated.group, serverDefinitions), {
      join: 'OR',
      rules: [],
      children: [{
        join: 'AND',
        rules: [
          { definitionCode: 'COUNTRY', operator: 'is_known', values: [] },
          { definitionCode: 'REPORTED_AGE', operator: 'is_unknown', values: [] }
        ]
      }]
    });
  });

  test(`Go no-value rules with ${wire.name} values humanize without a value suffix`, () => {
    assert.equal(
      audienceBuilder.humanizeFilterGroup(wire.definition, serverDefinitions),
      '(Country is known AND Reported age is unknown)'
    );
  });

  test(`Go no-value rules with ${wire.name} values compare equal only to empty values`, () => {
    assert.equal(audienceBuilder.filterGroupsEqual(wire.definition, {
      join: 'OR',
      rules: [],
      children: [{
        join: 'AND',
        rules: [
          { definitionCode: 'COUNTRY', operator: 'is_known', values: [] },
          { definitionCode: 'REPORTED_AGE', operator: 'is_unknown', values: [] }
        ]
      }]
    }), true);
    assert.equal(audienceBuilder.filterGroupsEqual(wire.definition, {
      join: 'OR',
      children: [{
        join: 'AND',
        rules: [
          { definitionCode: 'COUNTRY', operator: 'is_known', values: ['NG'] },
          { definitionCode: 'REPORTED_AGE', operator: 'is_unknown', values: [] }
        ]
      }]
    }), false);
  });
}

const valueRequiringWireCases = [
  { name: 'in null', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"in","values":null}]}') },
  { name: 'in omitted', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"in"}]}') },
  { name: 'not_in null', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"not_in","values":null}]}') },
  { name: 'not_in omitted', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"COUNTRY","operator":"not_in"}]}') },
  { name: 'equals null', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"equals","values":null}]}') },
  { name: 'equals omitted', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"equals"}]}') },
  { name: 'between null', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"between","values":null}]}') },
  { name: 'between omitted', definition: JSON.parse('{"join":"AND","rules":[{"definitionCode":"REPORTED_AGE","operator":"between"}]}') }
];

for (const wire of valueRequiringWireCases) {
  test(`hydrated ${wire.name} values still fail authoritative value-required validation`, () => {
    assert.throws(
      () => buildFilterGroup(hydrateEditableFilterGroup(wire.definition).group, serverDefinitions),
      /requires a value/
    );
  });
}

test('explicit empty values remain invalid for value-requiring operators', () => {
  for (const operator of ['in', 'not_in', 'equals', 'between']) {
    const definitionCode = operator === 'in' || operator === 'not_in' ? 'COUNTRY' : 'REPORTED_AGE';
    assert.throws(
      () => buildFilterGroup([{ id: 1, definitionCode, operator, values: [] }], serverDefinitions),
      /requires a value/
    );
  }
});

test('filter normalization recursively equates omitted arrays with explicit empty arrays', () => {
  assert.deepEqual(audienceBuilder.normalizeFilterGroup({
    join: 'AND',
    children: [{ join: 'OR', rules: [], children: [] }]
  }), {
    join: 'AND',
    rules: [],
    children: [{ join: 'OR', rules: [] }]
  });
  assert.deepEqual(audienceBuilder.normalizeFilterGroup({ join: 'OR', rules: [], children: [] }), {
    join: 'OR',
    rules: []
  });
});

test('filter normalization preserves rule order, child order, joins and primitive value types', () => {
  const input = {
    children: [
      { rules: [{ values: ['Silver', 'Gold'], operator: 'in', definitionCode: 'CUSTOMER_TIER' }], join: 'OR' as const },
      { join: 'AND' as const, rules: [{ definitionCode: 'REPORTED_AGE', operator: 'between', values: [18, 35] }] }
    ],
    rules: [
      { operator: 'in', values: ['02', 2, false, true], definitionCode: 'MIXED' },
      { definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] }
    ],
    join: 'AND' as const
  };
  assert.deepEqual(audienceBuilder.normalizeFilterGroup(input), {
    join: 'AND',
    rules: [
      { definitionCode: 'MIXED', operator: 'in', values: ['02', 2, false, true] },
      { definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] }
    ],
    children: [
      { join: 'OR', rules: [{ definitionCode: 'CUSTOMER_TIER', operator: 'in', values: ['Silver', 'Gold'] }] },
      { join: 'AND', rules: [{ definitionCode: 'REPORTED_AGE', operator: 'between', values: [18, 35] }] }
    ]
  });
  assert.deepEqual(input.children[0].rules[0].values, ['Silver', 'Gold']);
});

test('filter equality ignores empty-array omission and JSON property insertion order', () => {
  assert.equal(audienceBuilder.filterGroupsEqual(
    { join: 'AND', children: [{ join: 'OR', rules: [{ definitionCode: 'COUNTRY', operator: 'in', values: ['NG'] }] }] },
    { children: [{ rules: [{ values: ['NG'], operator: 'in', definitionCode: 'COUNTRY' }], children: [], join: 'OR' }], rules: [], join: 'AND' }
  ), true);
  assert.equal(audienceBuilder.filterGroupsEqual(
    { join: 'AND' },
    { join: 'AND', rules: [], children: [] }
  ), true);
});

test('filter equality rejects changes to joins, rules, children or value order and type', () => {
  const definition = {
    join: 'AND' as const,
    rules: [
      { definitionCode: 'COUNTRY', operator: 'in', values: ['NG', 'GH'] },
      { definitionCode: 'REPORTED_AGE', operator: 'between', values: [18, 35] }
    ],
    children: [
      { join: 'OR' as const, rules: [{ definitionCode: 'CUSTOMER_TIER', operator: 'in', values: ['Gold'] }] },
      { join: 'AND' as const, rules: [{ definitionCode: 'COUNTRY', operator: 'is_known', values: [] }] }
    ]
  };
  const alternatives = [
    { ...definition, join: 'OR' as const },
    { ...definition, rules: [{ ...definition.rules[0], definitionCode: 'CITY' }, definition.rules[1]] },
    { ...definition, rules: [{ ...definition.rules[0], operator: 'not_in' }, definition.rules[1]] },
    { ...definition, rules: [definition.rules[1], definition.rules[0]] },
    { ...definition, rules: [{ ...definition.rules[0], values: ['GH', 'NG'] }, definition.rules[1]] },
    { ...definition, rules: [definition.rules[0], { ...definition.rules[1], values: ['18', '35'] }] },
    { ...definition, children: [definition.children[1], definition.children[0]] },
    { ...definition, children: [definition.children[0]] },
    { ...definition, children: [{ ...definition.children[0], join: 'AND' as const }, definition.children[1]] }
  ];
  for (const alternative of alternatives) {
    assert.equal(audienceBuilder.filterGroupsEqual(definition, alternative), false);
  }
});

test('nested cohort tree fails closed at server-aligned depth and rule ceilings', () => {
  let root = createFilterGroupNode(1);
  let cursor = root;
  for (let depth = 2; depth <= maxFilterGroupDepth; depth += 1) {
    const child = createFilterGroupNode(depth);
    root = addChildGroup(root, cursor.id, child);
    cursor = child;
  }
  assert.equal(filterGroupDepth(root), maxFilterGroupDepth);
  assert.throws(
    () => addChildGroup(root, maxFilterGroupDepth, createFilterGroupNode(99)),
    /may not exceed 5 groups/i
  );

  let ruleRoot = createFilterGroupNode(200);
  for (let index = 0; index < maxFilterRules; index += 1) {
    ruleRoot = addRuleToGroup(ruleRoot, ruleRoot.id, {
      ...createRule(serverDefinitions[0], index + 1),
      values: ['NG']
    });
  }
  assert.equal(countFilterRules(ruleRoot), maxFilterRules);
  assert.throws(
    () => addRuleToGroup(ruleRoot, ruleRoot.id, { ...createRule(serverDefinitions[0], 999), values: ['NG'] }),
    /no more than 50 filter rules/i
  );
});

test('cohort estimate request requires governed organisation and purpose context', () => {
  const definition = buildFilterGroup([{ ...createRule(serverDefinitions[0], 1), values: ['NG'] }], serverDefinitions);
  assert.deepEqual(
    buildEstimateRequest(definition, { organisationId: 'org-1', purposeId: 'purpose-1', channel: 'WHATSAPP', clientRequestId: 'estimate-request-0001' }),
    {
      definition,
      organisationId: 'org-1',
      purposeId: 'purpose-1',
      channel: 'WHATSAPP',
      clientRequestId: 'estimate-request-0001'
    }
  );
  assert.throws(
    () => buildEstimateRequest(definition, { organisationId: '', purposeId: 'purpose-1', channel: 'WHATSAPP', clientRequestId: 'estimate-request-0002' }),
    /Organisation ID is required/i
  );
  assert.throws(
    () => buildEstimateRequest(definition, { organisationId: 'org-1', purposeId: 'purpose-1', channel: 'WHATSAPP', clientRequestId: 'short' }),
    /request ID.*8/i
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
