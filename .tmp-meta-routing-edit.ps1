$p = 'C:\Users\sanus\OpenWA\campaign-platform-active\repo\internal\execution\routing_plan.go'
$s = [IO.File]::ReadAllText($p).Replace("`r`n", "`n")
function Replace-Exact([string]$old, [string]$new) {
    if (-not $script:s.Contains($old)) { throw "missing expected routing block: $old" }
    $script:s = $script:s.Replace($old, $new)
}
Replace-Exact @'
var ErrRoutingPlanInvalid = errors.New("campaign routing plan is invalid")
'@ @'
var ErrRoutingPlanInvalid = errors.New("campaign routing plan is invalid")

type DistributionMode string

const (
	DistributionAuto DistributionMode = "AUTO"
	DistributionWeighted DistributionMode = "WEIGHTED"
)
'@
Replace-Exact @'
	GatewayPoolID             string `json:"gatewayPoolId"`
'@ @'
	GatewayPoolID             string `json:"gatewayPoolId,omitempty"`
	MetaSenderID              string `json:"metaSenderId,omitempty"`
'@
Replace-Exact @'
	Routes                  []PoolRoute `json:"routes"`
'@ @'
	Routes                  []PoolRoute `json:"routes"`
	DistributionMode        DistributionMode `json:"distributionMode"`
'@
Replace-Exact @'
	p.IdempotencyKey = strings.TrimSpace(p.IdempotencyKey)
	if p.CampaignID == "" || p.RoutingPolicyVersion == "" || p.CapacityEvidenceVersion == "" || p.PacingPolicyVersion == "" || p.ApprovedBy == "" || p.IdempotencyKey == "" || len(p.IdempotencyKey) > 200 || p.FallbackMode != "NONE" || len(p.Routes) == 0 || maximumRecipients < 1 {
'@ @'
	p.IdempotencyKey = strings.TrimSpace(p.IdempotencyKey)
	if p.DistributionMode == "" {
		p.DistributionMode = DistributionWeighted
	}
	if p.DistributionMode != DistributionAuto && p.DistributionMode != DistributionWeighted {
		return ErrRoutingPlanInvalid
	}
	if p.CampaignID == "" || p.RoutingPolicyVersion == "" || p.CapacityEvidenceVersion == "" || p.PacingPolicyVersion == "" || p.ApprovedBy == "" || p.IdempotencyKey == "" || len(p.IdempotencyKey) > 200 || p.FallbackMode != "NONE" || len(p.Routes) == 0 || maximumRecipients < 1 {
'@
Replace-Exact @'
		r.GatewayPoolID = strings.TrimSpace(r.GatewayPoolID)
		r.Provider = strings.ToUpper(strings.TrimSpace(r.Provider))
'@ @'
		r.GatewayPoolID = strings.TrimSpace(r.GatewayPoolID)
		r.MetaSenderID = strings.TrimSpace(r.MetaSenderID)
		r.Provider = strings.ToUpper(strings.TrimSpace(r.Provider))
'@
Replace-Exact @'
		if r.SenderPoolID == "" || r.GatewayPoolID == "" || r.Provider != "OPENWA" || (r.Engine != "WHATSAPP_WEB_JS" && r.Engine != "BAILEYS") || r.AllocationWeight < 1 || r.AllocationWeight > 10000 || r.MaximumRecipients < 1 || r.ReservedMessagesPerMinute < 1 || r.ReservedHourlyUnits < 1 || r.ReservedDailyUnits < r.ReservedHourlyUnits {
			return ErrRoutingPlanInvalid
		}
'@ @'
		endpointValid := false
		switch r.Provider {
		case "OPENWA":
			endpointValid = r.GatewayPoolID != "" && r.MetaSenderID == "" && (r.Engine == "WHATSAPP_WEB_JS" || r.Engine == "BAILEYS")
		case "META":
			endpointValid = r.GatewayPoolID == "" && r.MetaSenderID != "" && r.Engine == "CLOUD_API"
		}
		if r.SenderPoolID == "" || !endpointValid || r.AllocationWeight < 1 || r.AllocationWeight > 10000 || r.MaximumRecipients < 1 || r.ReservedMessagesPerMinute < 1 || r.ReservedHourlyUnits < 1 || r.ReservedDailyUnits < r.ReservedHourlyUnits {
			return ErrRoutingPlanInvalid
		}
'@
[IO.File]::WriteAllText($p, $s, [Text.UTF8Encoding]::new($false))
Write-Output 'routing-domain-minimal-edit-applied'
