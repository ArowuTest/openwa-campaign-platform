export type FilterDefinition = {
  code: string;
  displayName: string;
  description: string;
  dataType: string;
  operators: string[];
  allowedValues?: string[];
  sensitive?: boolean;
};

export type GeographyItem = {
  iso2?: string;
  code?: string;
  name: string;
  parentCode?: string;
};

export type Rule = {
  id: number;
  definitionCode: string;
  operator: string;
  values: string[];
};

export type FilterJoin = 'AND' | 'OR';

export type FilterGroup = {
  join: FilterJoin;
  rules?: Array<{
    definitionCode: string;
    operator: string;
    values?: Array<string | number | boolean> | null;
  }>;
  children?: FilterGroup[];
};

export type NormalizedFilterGroup = {
  join: FilterJoin;
  rules: Array<{
    definitionCode: string;
    operator: string;
    values: Array<string | number | boolean>;
  }>;
  children?: NormalizedFilterGroup[];
};

// Go omits empty group arrays; the editor always emits rules and omits empty children.
export function normalizeFilterGroup(group: FilterGroup): NormalizedFilterGroup {
  const children = (group.children ?? []).map(normalizeFilterGroup);
  return {
    join: group.join,
    rules: (group.rules ?? []).map((rule) => ({
      definitionCode: rule.definitionCode,
      operator: rule.operator,
      values: [...(rule.values ?? [])]
    })),
    ...(children.length ? { children } : {})
  };
}

// Ignore only wire-array omission and object key order, never rule/child/value order.
export function filterGroupsEqual(left: FilterGroup, right: FilterGroup): boolean {
  return JSON.stringify(normalizeFilterGroup(left)) === JSON.stringify(normalizeFilterGroup(right));
}

export type EditableFilterGroup = {
  id: number;
  join: FilterJoin;
  rules: Rule[];
  children: EditableFilterGroup[];
};

export const maxFilterGroupDepth = 5;
export const maxTotalFilterRules = 50;
export const maxFilterRules = maxTotalFilterRules;
export const maxValuesPerFilterRule = 500;

export type EstimateContext = {
  organisationId: string;
  purposeId: string;
  channel: 'WHATSAPP';
  clientRequestId: string;
};

export type EligibilityBreakdown = {
  matchedProfiles: number;
  consentEligible: number;
  consentExcluded: number;
  unsuppressed: number;
  suppressionExcluded: number;
  eligible: number;
  frequencyCapExcluded: number;
};

export type CohortEstimate = {
  eligibleCount: number;
  breakdown?: EligibilityBreakdown;
  calculatedAt: string;
  asOf?: string;
};

export type SegmentCreateContext = {
  organisationId: string;
  name: string;
  description?: string;
};

function cleanDefinitions(input: unknown): FilterDefinition[] {
  if (!Array.isArray(input) || input.length === 0) {
    throw new Error('The authoritative filter catalogue is unavailable or empty.');
  }
  const seen = new Set<string>();
  return input.map((raw) => {
    const item = raw as Partial<FilterDefinition>;
    const code = String(item.code ?? '').trim();
    const displayName = String(item.displayName ?? '').trim();
    const dataType = String(item.dataType ?? '').trim();
    const operators = Array.isArray(item.operators)
      ? item.operators.map((value) => String(value).trim()).filter(Boolean)
      : [];
    if (!code || !displayName || !dataType || operators.length === 0) {
      throw new Error('The authoritative filter catalogue contains an invalid definition.');
    }
    if (seen.has(code)) throw new Error('The authoritative filter catalogue contains duplicate definitions.');
    seen.add(code);
    return {
      code,
      displayName,
      description: String(item.description ?? '').trim(),
      dataType,
      operators,
      allowedValues: Array.isArray(item.allowedValues)
        ? item.allowedValues.map((value) => String(value).trim()).filter(Boolean)
        : [],
      sensitive: item.sensitive === true
    };
  });
}

function cleanCountries(input: unknown, definitions: readonly FilterDefinition[]): GeographyItem[] {
  if (!Array.isArray(input)) throw new Error('The authoritative geography catalogue is unavailable.');
  const countries = input.map((raw) => {
    const item = raw as Partial<GeographyItem>;
    const iso2 = String(item.iso2 ?? '').trim();
    const name = String(item.name ?? '').trim();
    if (!iso2 || !name) throw new Error('The authoritative geography catalogue contains an invalid country.');
    return { iso2, name };
  });
  if (definitions.some((item) => item.code === 'COUNTRY') && countries.length === 0) {
    throw new Error('The authoritative geography catalogue is empty.');
  }
  return countries;
}

export function requireAuthoritativeCatalogue(
  definitions: unknown,
  countries: unknown
): { definitions: FilterDefinition[]; countries: GeographyItem[] } {
  const clean = cleanDefinitions(definitions);
  return { definitions: clean, countries: cleanCountries(countries, clean) };
}

export function createRule(definition: FilterDefinition, id: number): Rule {
  const operator = definition.operators[0];
  return {
    id,
    definitionCode: definition.code,
    operator,
    values: operator === 'between' ? ['', ''] : operator.startsWith('is_') ? [] : ['']
  };
}

export function createEditableGroup(id: number, definition?: FilterDefinition, ruleId = 1): EditableFilterGroup {
  return {
    id,
    join: 'AND',
    rules: definition ? [createRule(definition, ruleId)] : [],
    children: []
  };
}

export const createFilterGroupNode = (id: number, join: FilterJoin = 'AND'): EditableFilterGroup => ({
  id,
  join,
  rules: [],
  children: []
});

// Compatibility name used by the React builder surface.
export const createEditableFilterGroup = createFilterGroupNode;

function mapGroup(
  root: EditableFilterGroup,
  targetId: number,
  updater: (group: EditableFilterGroup) => EditableFilterGroup
): EditableFilterGroup {
  if (root.id === targetId) return updater(root);
  return {
    ...root,
    children: root.children.map((child) => mapGroup(child, targetId, updater))
  };
}

function containsGroup(root: EditableFilterGroup, targetId: number): boolean {
  return root.id === targetId || root.children.some((child) => containsGroup(child, targetId));
}

export const countFilterRules = countRules;
export const filterGroupDepth = groupDepth;

export function updateGroupJoin(root: EditableFilterGroup, groupId: number, join: FilterJoin): EditableFilterGroup {
  if (join !== 'AND' && join !== 'OR') throw new Error('Filter groups must join rules with AND or OR.');
  if (!containsGroup(root, groupId)) throw new Error('Filter group no longer exists.');
  return mapGroup(root, groupId, (group) => ({ ...group, join }));
}

export function addRuleToGroup(root: EditableFilterGroup, groupId: number, rule: Rule): EditableFilterGroup {
  if (!containsGroup(root, groupId)) throw new Error('Filter group no longer exists.');
  if (countRules(root) >= maxTotalFilterRules) {
    throw new Error(`Cohort definitions may contain no more than ${maxTotalFilterRules} filter rules.`);
  }
  if (rule.id <= 0) throw new Error('Filter rule ID must be positive.');
  const duplicate = (group: EditableFilterGroup): boolean =>
    group.rules.some((item) => item.id === rule.id) || group.children.some(duplicate);
  if (duplicate(root)) throw new Error(`Filter rule ${rule.id} already exists.`);
  return mapGroup(root, groupId, (group) => ({ ...group, rules: [...group.rules, rule] }));
}

export function updateRuleInGroup(
  root: EditableFilterGroup,
  groupId: number,
  ruleId: number,
  updater: (rule: Rule) => Rule
): EditableFilterGroup {
  if (!containsGroup(root, groupId)) throw new Error('Filter group no longer exists.');
  let found = false;
  const next = mapGroup(root, groupId, (group) => ({
    ...group,
    rules: group.rules.map((rule) => {
      if (rule.id !== ruleId) return rule;
      found = true;
      return updater(rule);
    })
  }));
  if (!found) throw new Error('Filter rule no longer exists in this group.');
  return next;
}

export function removeRuleFromGroup(root: EditableFilterGroup, groupId: number, ruleId: number): EditableFilterGroup {
  if (!containsGroup(root, groupId)) throw new Error('Filter group no longer exists.');
  return mapGroup(root, groupId, (group) => ({
    ...group,
    rules: group.rules.filter((rule) => rule.id !== ruleId)
  }));
}

export function addChildGroup(root: EditableFilterGroup, parentId: number, child: EditableFilterGroup): EditableFilterGroup {
  if (!containsGroup(root, parentId)) throw new Error('Parent filter group no longer exists.');
  if (containsGroup(root, child.id)) throw new Error(`Filter group ${child.id} already exists.`);
  const next = mapGroup(root, parentId, (group) => ({ ...group, children: [...group.children, child] }));
  if (groupDepth(next) > maxFilterGroupDepth) {
    throw new Error(`Filter nesting may not exceed ${maxFilterGroupDepth} groups.`);
  }
  if (countRules(next) > maxTotalFilterRules) {
    throw new Error(`Cohort definitions may contain no more than ${maxTotalFilterRules} filter rules.`);
  }
  return next;
}

export function removeChildGroup(root: EditableFilterGroup, groupId: number): EditableFilterGroup {
  if (root.id === groupId) throw new Error('The root filter group cannot be removed.');
  const remove = (group: EditableFilterGroup): EditableFilterGroup => ({
    ...group,
    children: group.children
      .filter((child) => child.id !== groupId)
      .map(remove)
  });
  return remove(root);
}

function coerceValue(value: string, definition: FilterDefinition): string | number | boolean {
  if (definition.dataType === 'integer' || definition.dataType === 'decimal') {
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) throw new Error(`${definition.displayName} requires a valid number.`);
    if (definition.dataType === 'integer' && !Number.isInteger(parsed)) {
      throw new Error(`${definition.displayName} requires a whole number.`);
    }
    return parsed;
  }
  if (definition.dataType === 'boolean') {
    if (value === 'true') return true;
    if (value === 'false') return false;
    throw new Error(`${definition.displayName} requires true or false.`);
  }
  const trimmed = value.trim();
  if (!trimmed) throw new Error(`${definition.displayName} requires a value.`);
  return trimmed;
}

function convertRule(rule: Rule, definitions: readonly FilterDefinition[]) {
  const definition = definitions.find((item) => item.code === rule.definitionCode);
  if (!definition) throw new Error(`Filter definition ${rule.definitionCode} is no longer authoritative.`);
  if (!definition.operators.includes(rule.operator)) {
    throw new Error(`Operator ${rule.operator} is no longer allowed for ${definition.displayName}.`);
  }
  const requiresNoValue = rule.operator === 'is_known' || rule.operator === 'is_unknown';
  const rawValues = requiresNoValue ? [] : rule.values.filter((value) => value !== '');
  if (rawValues.length > maxValuesPerFilterRule) {
    throw new Error(`${definition.displayName} accepts no more than ${maxValuesPerFilterRule} values.`);
  }
  if (!requiresNoValue && rawValues.length === 0) {
    throw new Error(`${definition.displayName} requires a value.`);
  }
  if (rule.operator === 'between' && rawValues.length !== 2) {
    throw new Error(`${definition.displayName} requires both lower and upper bounds.`);
  }
  const values = rawValues.map((value) => coerceValue(value, definition));
  if (rule.operator === 'between' && Number(values[0]) > Number(values[1])) {
    throw new Error(`${definition.displayName} lower bound cannot exceed the upper bound.`);
  }
  return {
    definitionCode: definition.code,
    operator: rule.operator,
    values
  };
}

export function countRules(group: EditableFilterGroup): number {
  return group.rules.length + group.children.reduce((total, child) => total + countRules(child), 0);
}

export function groupDepth(group: EditableFilterGroup): number {
  if (group.children.length === 0) return 1;
  return 1 + Math.max(...group.children.map(groupDepth));
}

export function buildNestedFilterGroup(
  group: EditableFilterGroup,
  definitions: readonly FilterDefinition[],
  depth = 1,
  counter: { rules: number } = { rules: 0 }
): NormalizedFilterGroup {
  if (depth > maxFilterGroupDepth) {
    throw new Error(`Filter nesting cannot exceed ${maxFilterGroupDepth} levels.`);
  }
  if (group.join !== 'AND' && group.join !== 'OR') {
    throw new Error('Filter groups must join rules with AND or OR.');
  }
  if (group.rules.length === 0 && group.children.length === 0) {
    throw new Error('Every filter group must contain at least one rule or child group.');
  }
  counter.rules += group.rules.length;
  if (counter.rules > maxTotalFilterRules) {
    throw new Error(`Cohort definitions cannot exceed ${maxTotalFilterRules} rules.`);
  }
  return {
    join: group.join,
    rules: group.rules.map((rule) => convertRule(rule, definitions)),
    ...(group.children.length
      ? { children: group.children.map((child) => buildNestedFilterGroup(child, definitions, depth + 1, counter)) }
      : {})
  };
}

// Backward-compatible helper for the original single AND-group surface.
export function buildFilterGroup(
  input: readonly Rule[] | EditableFilterGroup,
  definitions: readonly FilterDefinition[]
): NormalizedFilterGroup {
  if (Array.isArray(input)) {
    return buildNestedFilterGroup({ id: 1, join: 'AND', rules: [...input], children: [] }, definitions);
  }
  return buildNestedFilterGroup(input as EditableFilterGroup, definitions);
}

export function buildEstimateRequest(definition: FilterGroup, context: EstimateContext) {
  const organisationId = context.organisationId.trim();
  const purposeId = context.purposeId.trim();
  const clientRequestId = context.clientRequestId.trim();
  if (!organisationId) throw new Error('Organisation ID is required for an authoritative estimate.');
  if (!purposeId) throw new Error('Consent purpose ID is required for an authoritative estimate.');
  if (clientRequestId.length < 8 || clientRequestId.length > 200) {
    throw new Error('Cohort estimate client request ID must contain 8-200 characters.');
  }
  return {
    definition,
    organisationId,
    purposeId,
    channel: context.channel,
    clientRequestId
  };
}

export function buildCreateSegmentRequest(definition: FilterGroup, context: SegmentCreateContext) {
  const organisationId = context.organisationId.trim();
  const name = context.name.trim();
  const description = String(context.description ?? '').trim();
  if (!organisationId) throw new Error('Organisation ID is required to save a governed segment.');
  if (!name) throw new Error('Segment name is required.');
  if (name.length > 160) throw new Error('Segment name must be 160 characters or fewer.');
  if (description.length > 2000) throw new Error('Segment description must be 2000 characters or fewer.');
  return {
    organisationId,
    name,
    ...(description ? { description } : {}),
    definition
  };
}

export function hydrateEditableFilterGroup(
  definition: FilterGroup,
  startId = 1
): { group: EditableFilterGroup; nextId: number } {
  let nextId = startId;
  const walk = (current: NormalizedFilterGroup): EditableFilterGroup => {
    const groupId = nextId++;
    const rules = current.rules.map((rule) => ({
      id: nextId++,
      definitionCode: rule.definitionCode,
      operator: rule.operator,
      values: rule.values.map((value) => String(value))
    }));
    const children = (current.children ?? []).map(walk);
    return { id: groupId, join: current.join, rules, children };
  };
  const group = walk(normalizeFilterGroup(definition));
  if (groupDepth(group) > maxFilterGroupDepth) {
    throw new Error(`Filter nesting cannot exceed ${maxFilterGroupDepth} levels.`);
  }
  if (countRules(group) > maxTotalFilterRules) {
    throw new Error(`Cohort definitions cannot exceed ${maxTotalFilterRules} rules.`);
  }
  return { group, nextId };
}

export function humanizeFilterGroup(group: FilterGroup, definitions: readonly FilterDefinition[]): string {
  const walk = (current: NormalizedFilterGroup): string => {
    const parts: string[] = [];
    for (const rule of current.rules) {
      const definition = definitions.find((item) => item.code === rule.definitionCode);
      const label = definition?.displayName ?? rule.definitionCode;
      const values = rule.values.length ? ' ' + rule.values.join(' and ') : '';
      parts.push(`${label} ${rule.operator.replaceAll('_', ' ')}${values}`);
    }
    for (const child of current.children ?? []) {
      parts.push('(' + walk(child) + ')');
    }
    return parts.join(` ${current.join} `);
  };
  return walk(normalizeFilterGroup(group));
}
