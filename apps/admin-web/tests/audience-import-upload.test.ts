import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildUploadSessionRequest,
  choosePreferredTransport,
  csvHeadersFromPrefix,
  fileMatchesResume,
  nextImportStage,
  sanitizeResumeRecord,
  uploadProgress
} from '../lib/audience-import-upload-model.ts';

test('bounded CSV header discovery handles BOM, quoted commas and normalizes blanks', () => {
  const headers = csvHeadersFromPrefix('\ufeffmsisdn,"display,name",country,state\r\n08012345678,Ada,NG,Lagos\r\n');
  assert.deepEqual(headers, ['msisdn', 'display,name', 'country', 'state']);
  assert.throws(() => csvHeadersFromPrefix('msisdn,country'), /complete header row/i);
});

test('resume record never includes source contents and matches exact local file identity', () => {
  const record = sanitizeResumeRecord({
    sessionId: 'session-1',
    requestKey: 'request-1234567890',
    fileName: 'audience.csv',
    fileSize: 30000015,
    fileLastModified: 123456,
    fileFingerprint: 'a'.repeat(64),
    organisationId: 'org-1',
    consentReviewId: 'review-1',
    purposeId: 'purpose-1',
    sourceName: 'Campaign audience',
    sourceSystem: 'CRM',
    extra: 'must-not-survive'
  } as never);
  assert.deepEqual(record, {
    sessionId: 'session-1',
    requestKey: 'request-1234567890',
    fileName: 'audience.csv',
    fileSize: 30000015,
    fileLastModified: 123456,
    fileFingerprint: 'a'.repeat(64),
    organisationId: 'org-1',
    consentReviewId: 'review-1',
    purposeId: 'purpose-1',
    sourceName: 'Campaign audience',
    sourceSystem: 'CRM'
  });
  assert.equal(fileMatchesResume({ name: 'audience.csv', size: 30000015, lastModified: 123456, fingerprint: 'a'.repeat(64) }, record), true);
  assert.equal(fileMatchesResume({ name: 'audience.csv', size: 30000016, lastModified: 123456, fingerprint: 'a'.repeat(64) }, record), false);
  assert.equal(fileMatchesResume({ name: 'audience.csv', size: 30000015, lastModified: 123456, fingerprint: 'b'.repeat(64) }, record), false);
});

test('upload request binds governed context and mapping without recipient data', () => {
  const payload = buildUploadSessionRequest({
    organisationId: 'org-1',
    consentReviewId: 'review-1',
    purposeId: 'purpose-1',
    sourceName: ' August registrations ',
    sourceSystem: ' CRM ',
    defaultCountryIso2: 'ng',
    originalFilename: 'audience.csv',
    expectedBytes: 123,
    templateVersion: 'audience-v1',
    updatePolicy: 'NEWEST_SOURCE',
    mapping: { msisdn: 'phone', country: 'country', state: '', lga: '', age: '', gender: '', worksheet: '' }
  });
  assert.equal(payload.sourceName, 'August registrations');
  assert.equal(payload.sourceSystem, 'CRM');
  assert.equal(payload.defaultCountryIso2, 'NG');
  assert.equal(payload.mapping.msisdn, 'phone');
  assert.equal(JSON.stringify(payload).includes('08012345678'), false);
  assert.throws(
    () => buildUploadSessionRequest({ ...payload, organisationId: '' } as never),
    /organisation/i
  );
});

test('transport selection prefers direct S3 only when advertised', () => {
  assert.equal(choosePreferredTransport(['DIRECT_S3', 'RELAY']), 'DIRECT_S3');
  assert.equal(choosePreferredTransport(['RELAY']), 'RELAY');
  assert.equal(choosePreferredTransport([]), 'RELAY');
});

test('progress and stage labels are server-authoritative', () => {
  assert.deepEqual(uploadProgress({ expectedBytes: 100, uploadedBytes: 25, partCount: 4, uploadedParts: 1 }), {
    percent: 25,
    bytesLabel: '25 B / 100 B',
    partsLabel: '1 / 4 parts'
  });
  assert.equal(nextImportStage('CREATED'), 'Upload source');
  assert.equal(nextImportStage('UPLOADED'), 'Security scan queued');
  assert.equal(nextImportStage('FINALISING'), 'Security scan and file verification');
  assert.equal(nextImportStage('IMPORT_CREATED'), 'Validation and reconciliation');
  assert.equal(nextImportStage('FAILED'), 'Failed');
});
