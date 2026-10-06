export type AudienceUploadTransport = 'DIRECT_S3' | 'RELAY';

export type AudienceImportColumnMapping = {
  worksheet?: string;
  msisdn: string;
  country: string;
  state: string;
  lga: string;
  age: string;
  gender: string;
};

export type UploadSessionRequestInput = {
  organisationId: string;
  consentReviewId: string;
  purposeId: string;
  sourceName: string;
  sourceSystem?: string;
  defaultCountryIso2?: string;
  originalFilename: string;
  expectedBytes: number;
  templateVersion: string;
  updatePolicy: 'INSERT_ONLY' | 'FILL_NULL' | 'NEWEST_SOURCE' | 'TRUSTED_SOURCE' | 'MANUAL_CONFLICT';
  mapping: AudienceImportColumnMapping;
};

export type UploadResumeRecord = {
  sessionId: string;
  requestKey: string;
  fileName: string;
  fileSize: number;
  fileLastModified: number;
  fileFingerprint: string;
  organisationId: string;
  consentReviewId: string;
  purposeId: string;
  sourceName: string;
  sourceSystem: string;
};

export type FileIdentity = {
  name: string;
  size: number;
  lastModified: number;
  fingerprint: string;
};

export const audienceImportResumeStorageKey = 'openwa.audienceImport.resume.v2';
export const csvHeaderProbeBytes = 64 << 10;

function parseCSVRecord(record: string): string[] {
  const values: string[] = [];
  let current = '';
  let quoted = false;
  for (let index = 0; index < record.length; index += 1) {
    const char = record[index];
    if (quoted) {
      if (char === '"') {
        if (record[index + 1] === '"') {
          current += '"';
          index += 1;
        } else {
          quoted = false;
        }
      } else {
        current += char;
      }
      continue;
    }
    if (char === '"') {
      quoted = true;
      continue;
    }
    if (char === ',') {
      values.push(current.trim());
      current = '';
      continue;
    }
    current += char;
  }
  if (quoted) throw new Error('CSV header contains an unterminated quoted value');
  values.push(current.trim());
  return values;
}

export function csvHeadersFromPrefix(prefix: string): string[] {
  const normalized = String(prefix ?? '').replace(/^\uFEFF/, '');
  const newline = normalized.search(/\r?\n/);
  if (newline < 0) throw new Error('A complete header row was not found in the bounded CSV probe');
  const header = normalized.slice(0, newline).replace(/\r$/, '');
  const values = parseCSVRecord(header).filter((value) => value.length > 0);
  if (values.length === 0) throw new Error('CSV header row is empty');
  const seen = new Set<string>();
  for (const value of values) {
    const key = value.trim().toLowerCase();
    if (seen.has(key)) throw new Error('CSV header contains duplicate column ' + value);
    seen.add(key);
  }
  return values;
}

export function sanitizeResumeRecord(value: Partial<UploadResumeRecord> & Record<string, unknown>): UploadResumeRecord {
  const record: UploadResumeRecord = {
    sessionId: String(value.sessionId ?? '').trim(),
    requestKey: String(value.requestKey ?? '').trim(),
    fileName: String(value.fileName ?? '').trim(),
    fileSize: Number(value.fileSize ?? 0),
    fileLastModified: Number(value.fileLastModified ?? 0),
    fileFingerprint: String(value.fileFingerprint ?? '').trim().toLowerCase(),
    organisationId: String(value.organisationId ?? '').trim(),
    consentReviewId: String(value.consentReviewId ?? '').trim(),
    purposeId: String(value.purposeId ?? '').trim(),
    sourceName: String(value.sourceName ?? '').trim(),
    sourceSystem: String(value.sourceSystem ?? '').trim()
  };
  if (
    !record.sessionId || !record.requestKey || !record.fileName ||
    !Number.isFinite(record.fileSize) || record.fileSize <= 0 ||
    !/^[0-9a-f]{64}$/.test(record.fileFingerprint)
  ) {
    throw new Error('Stored upload resume metadata is incomplete');
  }
  return record;
}

export function fileMatchesResume(file: FileIdentity, record: UploadResumeRecord): boolean {
  return file.name === record.fileName &&
    file.size === record.fileSize &&
    file.lastModified === record.fileLastModified &&
    file.fingerprint.toLowerCase() === record.fileFingerprint.toLowerCase();
}

export function buildUploadSessionRequest(input: UploadSessionRequestInput): UploadSessionRequestInput & { channel: 'WHATSAPP'; wordingVersion: string } {
  const organisationId = String(input.organisationId ?? '').trim();
  const consentReviewId = String(input.consentReviewId ?? '').trim();
  const purposeId = String(input.purposeId ?? '').trim();
  const sourceName = String(input.sourceName ?? '').trim();
  const originalFilename = String(input.originalFilename ?? '').trim();
  const templateVersion = String(input.templateVersion ?? '').trim();
  const msisdn = String(input.mapping?.msisdn ?? '').trim();
  if (!organisationId) throw new Error('Organisation is required');
  if (!consentReviewId) throw new Error('Approved consent review is required');
  if (!purposeId) throw new Error('Consent purpose is required');
  if (!sourceName) throw new Error('Source name is required');
  if (!originalFilename) throw new Error('Source file is required');
  if (!Number.isFinite(input.expectedBytes) || input.expectedBytes <= 0) throw new Error('Source file size must be positive');
  if (!templateVersion) throw new Error('Template version is required');
  if (!msisdn) throw new Error('MSISDN column mapping is required');

  return {
    organisationId,
    consentReviewId,
    purposeId,
    sourceName,
    sourceSystem: String(input.sourceSystem ?? '').trim(),
    defaultCountryIso2: String(input.defaultCountryIso2 ?? '').trim().toUpperCase(),
    originalFilename,
    expectedBytes: input.expectedBytes,
    templateVersion,
    updatePolicy: input.updatePolicy,
    mapping: {
      worksheet: String(input.mapping.worksheet ?? '').trim(),
      msisdn,
      country: String(input.mapping.country ?? '').trim(),
      state: String(input.mapping.state ?? '').trim(),
      lga: String(input.mapping.lga ?? '').trim(),
      age: String(input.mapping.age ?? '').trim(),
      gender: String(input.mapping.gender ?? '').trim()
    },
    channel: 'WHATSAPP',
    wordingVersion: 'v1'
  };
}

export function choosePreferredTransport(transports: readonly string[] | undefined): AudienceUploadTransport {
  if (transports?.includes('DIRECT_S3')) return 'DIRECT_S3';
  return 'RELAY';
}

function formatBytes(value: number): string {
  const bytes = Math.max(0, Number.isFinite(value) ? value : 0);
  if (bytes < 1024) return Math.round(bytes) + ' B';
  if (bytes < 1024 ** 2) return (bytes / 1024).toFixed(1) + ' KiB';
  if (bytes < 1024 ** 3) return (bytes / 1024 ** 2).toFixed(1) + ' MiB';
  return (bytes / 1024 ** 3).toFixed(2) + ' GiB';
}

export function uploadProgress(value: { expectedBytes: number; uploadedBytes: number; partCount: number; uploadedParts: number }) {
  const expected = Math.max(0, value.expectedBytes);
  const uploaded = Math.max(0, Math.min(value.uploadedBytes, expected));
  return {
    percent: expected > 0 ? Math.min(100, Math.round((uploaded / expected) * 100)) : 0,
    bytesLabel: formatBytes(uploaded) + ' / ' + formatBytes(expected),
    partsLabel: Math.max(0, value.uploadedParts) + ' / ' + Math.max(0, value.partCount) + ' parts'
  };
}

export function nextImportStage(state: string): string {
  switch (String(state ?? '').toUpperCase()) {
    case 'CREATED':
    case 'UPLOADING':
      return 'Upload source';
    case 'UPLOADED':
      return 'Security scan queued';
    case 'FINALISING':
      return 'Security scan and file verification';
    case 'IMPORT_CREATED':
      return 'Validation and reconciliation';
    case 'ABORTED':
      return 'Aborted';
    case 'EXPIRED':
      return 'Expired';
    case 'FAILED':
      return 'Failed';
    default:
      return 'Preparing';
  }
}
