"""Consumer checks for the bounded saved-campaign preparation contract."""
import unittest
from pathlib import Path
import yaml

ROOT = Path(__file__).resolve().parents[2]
SPEC = yaml.safe_load((ROOT / "contracts/openapi/control-api.yaml").read_text(encoding="utf-8"))

class CampaignPreparationContractTests(unittest.TestCase):
    def test_readiness_is_get_only_campaign_read(self):
        self.assertIn("/campaigns/{id}/readiness", SPEC["paths"])
        path = SPEC["paths"]["/campaigns/{id}/readiness"]
        self.assertEqual(set(path), {"get"})
        op = path["get"]
        self.assertEqual(op["operationId"], "getCampaignReadiness")
        self.assertIn("campaign.read", op["description"])
        self.assertNotIn("requestBody", op)
        self.assertEqual(op["responses"]["200"]["content"]["application/json"]["schema"], {"$ref": "#/components/schemas/CampaignReadiness"})
        for status, code in [("409", "CAMPAIGN_VERSION_CONFLICT"), ("409", "CAMPAIGN_READINESS_STAGE_UNSUPPORTED"), ("503", "CAMPAIGN_READINESS_UNAVAILABLE")]:
            self.assertIn(code, op["responses"][status]["description"])
        self.assertIn("no-store", op["responses"]["200"]["headers"]["Cache-Control"]["schema"]["const"])
    def test_readiness_schema_safe_arrays_and_closed_objects(self):
        schemas = SPEC["components"]["schemas"]
        for name in ["CampaignReadiness", "ReadinessCheck", "EvidenceReference", "CapacityPreview"]:
            self.assertIn(name, schemas)
            self.assertIs(schemas[name]["additionalProperties"], False)
        r = schemas["CampaignReadiness"]
        self.assertEqual(set(r["required"]), {"campaignId","campaignVersion","campaignStatus","assessedAt","state","readyForFinalReview","checks","limitations"})
        self.assertEqual(r["properties"]["state"]["enum"], ["BLOCKED","REQUIRES_REVIEW","READY_FOR_FINAL_REVIEW"])
        self.assertEqual(schemas["ReadinessCheck"]["properties"]["status"]["enum"], ["PASS","BLOCKED","PENDING_REVIEW","UNAVAILABLE","NOT_APPLICABLE"])
        self.assertIn("evidence", schemas["ReadinessCheck"]["required"])
        self.assertEqual(schemas["ReadinessCheck"]["properties"]["evidence"]["type"], "array")
        self.assertEqual(schemas["CapacityPreview"]["properties"]["reasons"]["type"], "array")
        for key in ["healthySessions","healthyNodes","minimumHealthyNodes","availableMessagesPerMinute","availableHourlyUnits","availableDailyUnits","basisRecipientCount"]:
            self.assertEqual(schemas["CapacityPreview"]["properties"][key]["maximum"], 9007199254740991)
        self.assertEqual(schemas["EvidenceReference"]["properties"]["version"]["type"], "integer")
        self.assertEqual(schemas["CapacityPreview"]["properties"]["classification"]["enum"], ["ADVISORY_ONLY","BLOCKED"])
        self.assertNotIn("launchToken", r["properties"])
        for field in ["assessedAt", "effectiveStartAt"]:
            self.assertEqual(r["properties"][field]["type"], "string")
            self.assertEqual(r["properties"][field]["format"], "date-time")
    def test_prior_detail_and_draft_contract_remains_exact(self):
        schemas = SPEC["components"]["schemas"]
        self.assertFalse(schemas["CampaignDraftSaveRequest"]["additionalProperties"])
        self.assertIn("expectedVersion",schemas["CampaignDraftSaveRequest"]["required"])
        self.assertNotIn("requestedStartAt",schemas["CampaignDraftSaveRequest"]["required"])
        self.assertEqual(set(schemas["CampaignDraftSaveRequest"]["properties"]["requestedStartAt"]["type"]), {"string","null"})
        self.assertEqual(SPEC["paths"]["/campaigns/{id}/draft"]["put"]["requestBody"]["required"], True)

if __name__ == "__main__":
    unittest.main()
