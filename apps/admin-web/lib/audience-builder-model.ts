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

export type FilterGroup = {
  join: 'AND';
  rules: Array<{
    definitionCode: string;
    operator: string;
    values: Array<string | number | boolean>;
  }>;
};

export type EstimateContext = {
  organisationId: string;
  purposeId: string;
  channel: 'WHATSAPP';
};

export type CohortEstimate = {
  eligibleCount: number;
  calculatedAt: string;
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

function coerceValue(value: string, definition: FilterDefinition): string | number | boolean {
  if (definition.dataType === 'integer' || definition.dataType === 'decimal') {
    const parsed = Number(value);
    if (!Number.isFinite(parsed)) throw new Error(`${definition.displayName} requires a valid number.`);
    return parsed;
  }
  if (definition.dataType === 'boolean') {
    if (value === 'true') return true;
    if (value === 'false') return false;
    throw new Error(`${definition.displayName} requires true or false.`);
  }
  return value;
}

export function buildFilterGroup(rules: readonly Rule[], definitions: readonly FilterDefinition[]): FilterGroup {
  if (rules.length === 0) throw new Error('Add at least one authoritative filter before validation.');
  return {
    join: 'AND',
    rules: rules.map((rule) => {
      const definition = definitions.find((item) => item.code === rule.definitionCode);
      if (!definition) throw new Error(`Filter definition ${rule.definitionCode} is no longer authoritative.`);
      if (!definition.operators.includes(rule.operator)) {
        throw new Error(`Operator ${rule.operator} is no longer allowed for ${definition.displayName}.`);
      }
      const requiresNoValue = rule.operator === 'is_known' || rule.operator === 'is_unknown';
      return {
        definitionCode: definition.code,
        operator: rule.operator,
        values: requiresNoValue
          ? []
          : rule.values.filter((value) => value !== '').map((value) => coerceValue(value, definition))
      };
    })
  };
}

export function buildEstimateRequest(definition: FilterGroup, context: EstimateContext) {
  const organisationId = context.organisationId.trim();
  const purposeId = context.purposeId.trim();
  if (!organisationId) throw new Error('Organisation ID is required for an authoritative estimate.');
  if (!purposeId) throw new Error('Consent purpose ID is required for an authoritative estimate.');
  return {
    definition,
    organisationId,
    purposeId,
    channel: context.channel
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
