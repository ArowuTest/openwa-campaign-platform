import { expect,test } from 'vitest';
import { APIError } from '../lib/api';
test('APIError preserves the canonical Go error code and safe details',()=>{
 const failure=new APIError(503,JSON.parse('{"error":"CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE","message":"Governance unavailable.","details":{"field":"PURPOSE"}}'));
 expect(failure.code).toBe('CAMPAIGN_DRAFT_GOVERNANCE_UNAVAILABLE');expect(failure.message).toBe('Governance unavailable.');expect(failure.details).toEqual({field:'PURPOSE'});
});
test('APIError retains legacy proxy code compatibility and prefers canonical server error',()=>{
 expect(new APIError(503,{code:'CONTROL_API_UNAVAILABLE'}).code).toBe('CONTROL_API_UNAVAILABLE');
 expect(new APIError(409,JSON.parse('{"error":"CAMPAIGN_VERSION_CONFLICT","code":"LEGACY"}')).code).toBe('CAMPAIGN_VERSION_CONFLICT');
});
