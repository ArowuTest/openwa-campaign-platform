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

export function getCampaignDetail(campaignId: string, signal?: AbortSignal): Promise<CampaignDetail> {
  return apiRequest<CampaignDetail>('/v1/campaigns/' + encodeURIComponent(campaignId), { signal });
}
