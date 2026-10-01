#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');
const ts = require(path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript'));
const root = path.join(process.cwd(), 'services/openwa-gateway/src');
const nest = { Injectable: () => x => x, Inject: () => () => {}, Logger: class { warn() {} },
 ConflictException: class extends Error {}, UnauthorizedException: class extends Error {}, ServiceUnavailableException: class extends Error {} };
function load(name) {
 const filename=path.join(root,name); const output=ts.transpileModule(fs.readFileSync(filename,'utf8'),{fileName:filename,compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS,experimentalDecorators:true,esModuleInterop:true}}).outputText;
 const m={exports:{}}; const req=s=>s==='@nestjs/common'?nest:s.startsWith('.')?load(path.relative(root,path.resolve(path.dirname(filename),s+'.ts'))):require(s);
 new Function('exports','require','module',output)(m.exports,req,m);return m.exports;
}
const {RuntimeRegistrationService}=load('runtime-registration.service.ts');
const {SessionAuthorityService}=load('session-authority.service.ts');
const {GatewayMessagingService}=load('gateway-messaging.service.ts');
const identity={provider:'OPENWA',engine:'BAILEYS',gatewayPoolId:'pool-1',gatewayPoolVersion:4,adapterVersion:'0.8.28',nodeId:'node-1',nodeVersion:9};
const metrics={increment(){},gauge(){},observe(){},traceparent(){}};
function request(version=5) {return {idempotencyKey:'ownership-0000000000000001',...identity,gatewayNodeId:identity.nodeId,gatewayNodeVersion:9,gatewayAdapterVersion:identity.adapterVersion,sessionId:'session-1',sessionLeaseVersion:version,sessionConfigurationVersion:2,authorityExpiresAt:new Date(Date.now()+60000).toISOString(),routeReference:'campaign:recipient',recipientMsisdn:'+2348000000000',messageType:'text',body:'test',clientReference:'test'};}
async function fixture(t) {
 const dir=await fsp.mkdtemp(path.join(os.tmpdir(),'openwa-owned-')); const old=process.env.GATEWAY_SESSION_AUTHORITY_DIR;
 process.env.GATEWAY_SESSION_AUTHORITY_DIR=dir;
 const runtime=new RuntimeRegistrationService(identity,metrics);runtime.runtimeRegistered=true;
 const authority=new SessionAuthorityService(identity,runtime);await authority.onModuleInit();
 const count={start:0,send:0,idempotency:0};
 const provider={health:async()=>({ready:true,status:'READY'}),startSession:async()=>{count.start++;return {id:'session-1'}},send:async()=>{count.send++;return {accepted:true}}};
 const pipelines={beginStart(){},finishStart(){},run:async(_,f)=>f(),status:()=>({}),recordProviderSuccess(){},recordProviderFailure(){}};
 const gateway=new GatewayMessagingService(provider,{execute:async(_,f)=>{count.idempotency++;return f()}},pipelines,authority);
 t.after(async()=>{runtime.revokeSessionOwnership?.('session-1');await fsp.rm(dir,{recursive:true,force:true});if(old===undefined)delete process.env.GATEWAY_SESSION_AUTHORITY_DIR;else process.env.GATEWAY_SESSION_AUTHORITY_DIR=old;});
 return {runtime,authority,gateway,count,provider,pipelines};
}
async function publish(runtime,{version=5,status='READY',modify=x=>x,delay}={}){
 const oldFetch=global.fetch,oldURL=process.env.CONTROL_API_INTERNAL_URL;
 process.env.CONTROL_API_INTERNAL_URL='http://control.test';
 global.fetch=async()=>{if(delay)await delay();const now=Date.now();return new Response(JSON.stringify(modify({sentToday:7,status,ownership:{nodeId:'node-1',sessionId:'session-1',bootId:runtime.bootIdentity(),leaseVersion:version,leaseExpiresAt:new Date(now+90000).toISOString(),serverNow:new Date(now).toISOString()}})),{status:200});};
 try{return await runtime.publishSessionHeartbeat('session-1',{nodeId:'node-1',sessionId:'session-1',bootId:runtime.bootIdentity(),status,engineVersion:'test',sentToday:7});}
 finally{global.fetch=oldFetch;if(oldURL===undefined)delete process.env.CONTROL_API_INTERNAL_URL;else process.env.CONTROL_API_INTERNAL_URL=oldURL;}
}

test('unowned process must not activate provider',async t=>{const f=await fixture(t);let error;try{await f.gateway.startSession('session-1')}catch(e){error=e}assert.equal(f.count.start,0);assert.ok(error);});
test('empty authority directory cannot promote prior-boot authority into permission to send',async t=>{const f=await fixture(t);let error;try{await f.gateway.send(request())}catch(e){error=e}assert.equal(f.count.send,0);assert.equal(f.count.idempotency,0);assert.ok(error);});
test('accepted current-boot receipt enables start and matching send',async t=>{const f=await fixture(t);await publish(f.runtime);await f.gateway.startSession('session-1');await f.gateway.send(request());assert.deepEqual(f.count,{start:1,send:1,idempotency:1});});
test('previous boot dispatch version below first accepted receipt is rejected',async t=>{const f=await fixture(t);await publish(f.runtime,{version:6});await assert.rejects(()=>f.gateway.send(request(5)));assert.equal(f.count.idempotency,0);assert.equal(f.count.send,0);});
test('unacknowledged future lease version is rejected',async t=>{const f=await fixture(t);await publish(f.runtime,{version:5});await assert.rejects(()=>f.gateway.send(request(6)));assert.equal(f.count.idempotency,0);});
test('non-sending recovery ownership permits start but not send',async t=>{const f=await fixture(t);await publish(f.runtime,{status:'DISCONNECTED'});await f.gateway.startSession('session-1');await assert.rejects(()=>f.gateway.send(request()));assert.equal(f.count.start,1);assert.equal(f.count.send,0);});
test('revocation while queued prevents idempotency and provider submission', async t => {
 const f = await fixture(t);
 await publish(f.runtime);
 let enteredQueue = false;
 f.pipelines.run = async (_, operation) => {
  enteredQueue = true;
  f.runtime.revokeSessionOwnership('session-1');
  return operation();
 };
 await assert.rejects(() => f.gateway.send(request()), /current gateway process does not own a live session lease/);
 assert.equal(enteredQueue, true, 'test must reach the queue before revoking ownership');
 assert.equal(f.count.idempotency, 0);
 assert.equal(f.count.send, 0);
});
test('revocation during final health lookup prevents idempotency boundary', async t => {
 const f = await fixture(t);
 await publish(f.runtime);
 let checks = 0;
 f.provider.health = async () => {
  if (++checks === 2) f.runtime.revokeSessionOwnership('session-1');
  return { ready: true, status: 'READY' };
 };
 await assert.rejects(() => f.gateway.send(request()), /current gateway process does not own a live session lease/);
 assert.equal(checks, 2, 'test must reach the final provider health check');
 assert.equal(f.count.idempotency, 0);
 assert.equal(f.count.send, 0);
});
for(const [name,modify] of Object.entries({missing:x=>{delete x.ownership;return x},wrongBoot:x=>{x.ownership.bootId='old-boot';return x},wrongNode:x=>{x.ownership.nodeId='other-node';return x},wrongSession:x=>{x.ownership.sessionId='other-session';return x},expired:x=>{x.ownership.leaseExpiresAt=x.ownership.serverNow;return x},tooLong:x=>{x.ownership.leaseExpiresAt=new Date(Date.parse(x.ownership.serverNow)+600001).toISOString();return x},badVersion:x=>{x.ownership.leaseVersion=0;return x}})){
 test('invalid ownership receipt: '+name,async t=>{const f=await fixture(t);await assert.rejects(()=>publish(f.runtime,{modify}));await assert.rejects(()=>f.gateway.startSession('session-1'));assert.equal(f.count.start,0);});
}
test('rejected heartbeat immediately invalidates existing ownership',async t=>{const f=await fixture(t);await publish(f.runtime);const old=global.fetch;global.fetch=async()=>new Response('{"error":"OWNERSHIP_CONFLICT"}',{status:409});const url=process.env.CONTROL_API_INTERNAL_URL;process.env.CONTROL_API_INTERNAL_URL='http://control.test';try{await assert.rejects(()=>f.runtime.publishSessionHeartbeat('session-1',{nodeId:'node-1',sessionId:'session-1',bootId:f.runtime.bootIdentity(),status:'READY',engineVersion:'test',sentToday:7}));}finally{global.fetch=old;if(url===undefined)delete process.env.CONTROL_API_INTERNAL_URL;else process.env.CONTROL_API_INTERNAL_URL=url;}await assert.rejects(()=>f.gateway.send(request()));assert.equal(f.count.send,0);});
test('ownership loss listener runs when proof is revoked',async t=>{const f=await fixture(t);let lost=0;const remove=f.runtime.onSessionOwnershipLost(()=>{lost++});await publish(f.runtime);f.runtime.revokeSessionOwnership('session-1');assert.equal(lost,1);remove();});

function registryProof(status='DISCONNECTED',version=1) {const now=Date.now();return {status,ownership:{nodeId:'node-1',sessionId:'session-1',bootId:'boot-1',leaseVersion:version,leaseExpiresAt:new Date(now+90000).toISOString(),serverNow:new Date(now).toISOString()}};}
test('ownership registry cannot renew across an unobserved local expiry without retirement',()=>{
 const {SessionOwnershipRegistry}=load('session-ownership.ts');const r=new SessionOwnershipRegistry();let retired=0;r.onLoss(()=>retired++);
 r.accept('session-1','node-1','boot-1',r.begin('session-1'),registryProof());
 // Simulate timer scheduling delay: the lease has expired before its callback ran.
 r.sessions.get('session-1').deadline=-1;
 try{assert.throws(()=>r.accept('session-1','node-1','boot-1',r.begin('session-1'),registryProof('DISCONNECTED',2)),/expired|recover|ownership/);assert.equal(retired,1);}finally{r.revokeAll();}
});
test('ownership registry rejects responses superseded by revocation',()=>{
 const {SessionOwnershipRegistry}=load('session-ownership.ts');const r=new SessionOwnershipRegistry();const pending=r.begin('session-1');r.revoke('session-1');
 assert.throws(()=>r.accept('session-1','node-1','boot-1',pending,registryProof()),/superseded/);assert.throws(()=>r.assertOwned('session-1'));
});
test('ownership registry permits older same-boot dispatch only within the accepted epoch',()=>{
 const {SessionOwnershipRegistry}=load('session-ownership.ts');const r=new SessionOwnershipRegistry();
 try{r.accept('session-1','node-1','boot-1',r.begin('session-1'),registryProof('READY',5));r.accept('session-1','node-1','boot-1',r.begin('session-1'),registryProof('READY',6));r.assertOwned('session-1',5);r.assertOwned('session-1',6);assert.throws(()=>r.assertOwned('session-1',4));assert.throws(()=>r.assertOwned('session-1',7));}finally{r.revokeAll();}
});
