import { apiRequest } from './api';

export type CampaignTransport = {
  channel: string;
  provider: string;
  engine: string;
  routingMode: string;
  gatewayPoolId?: string;
  gatewayPoolVersion?: number;
  metaSenderId?: string;
  sessionId?: string;
  senderPoolId?: string;
  adapterVersion: string;
  providerDefinitionId?: string;
  providerDefinitionVersion?: number;
  requiredCapabilities?: string[];
  fallbackMode: string;
  routingPolicyVersion: string;
  capacityEvidenceVersion: string;
};

export type CampaignDetail = {
  id: string;
  organisationId: string;
  name: string;
  purposeId: string;
  consentReviewId: string;
  status: string;
  requestedStartAt?: string;
  completionDeadlineAt?: string;
  timezone: string;
  quietHoursStart?: string;
  quietHoursEnd?: string;
  maximumUniqueRecipients: number;
  maximumMessagesPerRecipient: number;
  audienceSnapshotId?: string;
  audienceSnapshotHash?: string;
  eligibleAudienceCount: number;
  messageVersionId?: string;
  messageContentHash?: string;
  senderPool?: string;
  transport: CampaignTransport;
  createdBy: string;
  finalApprovedBy?: string;
  commercialApprovalId?: string;
  pauseReason?: string;
  createdAt: string;
  updatedAt: string;
  version: number;
};

// Preserve opaque fixture/domain IDs; only UUID spellings share a canonical case.
export function canonicalUUID(value: string): string {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value) ? value.toLowerCase() : value;
}
function campaignForRequest(campaign: CampaignDetail, campaignId: string): CampaignDetail {
  const value = { ...campaign, transport: { ...campaign.transport } };
  for (const key of ['id', 'organisationId', 'purposeId', 'consentReviewId', 'audienceSnapshotId', 'messageVersionId', 'senderPool', 'createdBy', 'finalApprovedBy', 'commercialApprovalId'] as const) {
    const field = value[key];
    if (typeof field === 'string') value[key] = canonicalUUID(field);
  }
  for (const key of ['gatewayPoolId', 'senderPoolId', 'providerDefinitionId', 'sessionId', 'metaSenderId'] as const) {
    const field = value.transport[key];
    if (typeof field === 'string') value.transport[key] = canonicalUUID(field);
  }
  if (value.id !== canonicalUUID(campaignId)) throw new Error('Campaign response identity does not match the request.');
  return value;
}
export async function getCampaignDetail(campaignId: string, signal?: AbortSignal): Promise<CampaignDetail> {
  const value = await apiRequest<CampaignDetail>('/v1/campaigns/' + encodeURIComponent(canonicalUUID(campaignId)), { signal });
  return campaignForRequest(value, campaignId);
}

export type DraftTransportRequest = {
  channel: 'WHATSAPP';
  provider: 'OPENWA';
  engine: 'WHATSAPP_WEB_JS' | 'BAILEYS';
  routingMode: 'SENDER_POOL';
  gatewayPoolId: string;
  senderPoolId: string;
  adapterVersion: string;
  routingPolicyVersion: string;
  capacityEvidenceVersion: string;
  fallbackMode: 'NONE';
  requiredCapabilities: string[];
};
export type DraftSaveRequest = {
  expectedVersion: number;
  reason: string;
  name: string;
  organisationId: string;
  purposeId: string;
  consentReviewId: string;
  maximumUniqueRecipients: number;
  maximumMessagesPerRecipient: 1;
  requestedStartAt: string | null;
  completionDeadlineAt: string | null;
  timezone: string;
  quietHoursStart: string;
  quietHoursEnd: string;
  transport: DraftTransportRequest;
};
export function canonicalDraftSaveRequest(input: DraftSaveRequest): DraftSaveRequest {
  return { ...input, organisationId: canonicalUUID(input.organisationId), purposeId: canonicalUUID(input.purposeId), consentReviewId: canonicalUUID(input.consentReviewId),
    transport: { ...input.transport, gatewayPoolId: canonicalUUID(input.transport.gatewayPoolId), senderPoolId: canonicalUUID(input.transport.senderPoolId) } };
}
export async function saveCampaignDraft(campaignId: string, input: DraftSaveRequest): Promise<CampaignDetail> {
  const value = await apiRequest<CampaignDetail>('/v1/campaigns/' + encodeURIComponent(canonicalUUID(campaignId)) + '/draft', { method: 'PUT', body: JSON.stringify(canonicalDraftSaveRequest(input)) });
  return campaignForRequest(value, campaignId);
}
