import { apiRequest } from './api';

export const readinessKeys = ['organisation', 'purpose', 'consent', 'audience', 'message', 'transport', 'schedule', 'capacity', 'pilot', 'commercial', 'maintenance', 'finalReview'] as const;
export type ReadinessCheck = { key: string; status: 'PASS' | 'BLOCKED' | 'PENDING_REVIEW' | 'UNAVAILABLE' | 'NOT_APPLICABLE'; code: string; message: string; remediation: string; evidence: EvidenceReference[] };
export type EvidenceReference = { kind: string; id: string; version?: number; hash?: string; status?: string; observedAt?: string };
export type CapacityPreview = {
 senderPoolId: string; gatewayPoolId: string; healthySessions: number; healthyNodes: number; minimumHealthyNodes: number;
 availableMessagesPerMinute: number; availableHourlyUnits: number; availableDailyUnits: number; safetyMarginPercent: number;
 basisRecipientCount: number; basis: 'SNAPSHOT' | 'AUTHORISED_MAXIMUM'; requiredMessagesPerMinute?: number; projectedCompletionAt?: string;
 measurementAsOf: string; classification: 'ADVISORY_ONLY' | 'BLOCKED'; reasons: string[];
};
export type CampaignReadiness = {
 campaignId: string; campaignVersion: number; campaignStatus: string; assessedAt: string; effectiveStartAt?: string;
 state: 'BLOCKED' | 'REQUIRES_REVIEW' | 'READY_FOR_FINAL_REVIEW'; readyForFinalReview: boolean;
 checks: ReadinessCheck[]; capacity?: CapacityPreview; limitations: string[];
};
function object(value: unknown, required: string[], optional: string[] = []): Record<string, unknown> {
 if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid readiness evidence.');
 const obj = value as Record<string, unknown>;
 if (required.some(key => !(key in obj)) || Object.keys(obj).some(key => !required.includes(key) && !optional.includes(key))) throw new Error('Invalid readiness evidence.');
 return obj;
}
function text(value: unknown): value is string { return typeof value === 'string' && value.length > 0; }
function integer(value: unknown, min = 0): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= min; }
function date(value: unknown): value is string { return text(value) && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$/.test(value) && Number.isFinite(Date.parse(value)) && new Date(value).toISOString().slice(0,19) === value.slice(0,19); }
function requireValue(valid: boolean) { if (!valid) throw new Error('Invalid readiness evidence.'); }
function strings(value: unknown): value is string[] { return Array.isArray(value) && value.every(text); }
export function validateCampaignReadiness(value: unknown): CampaignReadiness {
 const r = object(value, ['campaignId','campaignVersion','campaignStatus','assessedAt','state','readyForFinalReview','checks','limitations'], ['effectiveStartAt','capacity']);
 requireValue(text(r.campaignId) && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(r.campaignId) && integer(r.campaignVersion,1));
 requireValue(text(r.campaignStatus) && ['DRAFT','CONSENT_REVIEW_PENDING','CONSENT_APPROVED','AUDIENCE_BUILDING','AUDIENCE_VALIDATED','MESSAGE_REVIEW_PENDING','MESSAGE_APPROVED','COMMERCIAL_APPROVED','FINAL_APPROVAL_PENDING'].includes(String(r.campaignStatus)));
 requireValue(date(r.assessedAt) && (!('effectiveStartAt' in r) || date(r.effectiveStartAt)));
 requireValue(text(r.state) && ['BLOCKED','REQUIRES_REVIEW','READY_FOR_FINAL_REVIEW'].includes(String(r.state)) && typeof r.readyForFinalReview === 'boolean' && r.readyForFinalReview === (r.state === 'READY_FOR_FINAL_REVIEW'));
 requireValue(strings(r.limitations) && r.limitations.length > 0 && r.limitations.includes('Readiness is advisory; final approval revalidates current evidence.'));
 requireValue(Array.isArray(r.checks) && r.checks.length === readinessKeys.length);
 for (const [index, check] of (r.checks as unknown[]).entries()) {
  const c = object(check, ['key','status','code','message','remediation','evidence']);
  requireValue(c.key === readinessKeys[index] && text(c.status) && ['PASS','BLOCKED','PENDING_REVIEW','UNAVAILABLE','NOT_APPLICABLE'].includes(String(c.status)) && text(c.code) && /^[A-Z][A-Z0-9_]+$/.test(c.code) && text(c.message) && text(c.remediation) && Array.isArray(c.evidence));
  for (const reference of c.evidence as unknown[]) {
   const e = object(reference, ['kind','id'], ['version','hash','status','observedAt']);
   requireValue(text(e.kind) && text(e.id) && (!('version' in e) || integer(e.version,1)) && (!('hash' in e) || text(e.hash)) && (!('status' in e) || text(e.status)) && (!('observedAt' in e) || date(e.observedAt)));
  }
 }
 const checks = r.checks as ReadinessCheck[];
 const derived = checks.some(c => c.status === 'BLOCKED' || c.status === 'UNAVAILABLE') ? 'BLOCKED' : checks.some(c => c.status === 'PENDING_REVIEW') ? 'REQUIRES_REVIEW' : 'READY_FOR_FINAL_REVIEW';
 requireValue(r.state === derived && (r.state !== 'READY_FOR_FINAL_REVIEW' || r.campaignStatus === 'COMMERCIAL_APPROVED' || r.campaignStatus === 'FINAL_APPROVAL_PENDING'));
 if ('capacity' in r) {
  const p = object(r.capacity, ['senderPoolId','gatewayPoolId','healthySessions','healthyNodes','minimumHealthyNodes','availableMessagesPerMinute','availableHourlyUnits','availableDailyUnits','safetyMarginPercent','basisRecipientCount','basis','measurementAsOf','classification','reasons'], ['requiredMessagesPerMinute','projectedCompletionAt']);
  requireValue(text(p.senderPoolId) && text(p.gatewayPoolId) && text(p.basis) && ['SNAPSHOT','AUTHORISED_MAXIMUM'].includes(String(p.basis)) && date(p.measurementAsOf) && text(p.classification) && ['ADVISORY_ONLY','BLOCKED'].includes(String(p.classification)) && strings(p.reasons));
  for (const key of ['healthySessions','healthyNodes','minimumHealthyNodes','availableMessagesPerMinute','availableHourlyUnits','availableDailyUnits','safetyMarginPercent','basisRecipientCount']) requireValue(integer(p[key]));
  requireValue(Number(p.safetyMarginPercent) <= 90 && (!('requiredMessagesPerMinute' in p) || typeof p.requiredMessagesPerMinute === 'number' && Number.isFinite(p.requiredMessagesPerMinute) && p.requiredMessagesPerMinute > 0));
  requireValue(!('projectedCompletionAt' in p) || date(p.projectedCompletionAt) && date(r.effectiveStartAt) && Date.parse(p.projectedCompletionAt) >= Date.parse(r.effectiveStartAt));
  requireValue((p.reasons as string[]).includes('RESERVATION_WINDOW_FEASIBILITY_NOT_ASSESSED') && (r.limitations as string[]).includes('These live capacity figures do not assess competing campaign reservations over the saved window. They do not establish reserved-window or deadline feasibility.'));
 }
 return value as CampaignReadiness;
}
export async function getCampaignReadiness(campaignId: string, signal?: AbortSignal): Promise<CampaignReadiness> {
 return validateCampaignReadiness(await apiRequest<unknown>('/v1/campaigns/' + encodeURIComponent(campaignId) + '/readiness', { signal }));
}
