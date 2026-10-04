#!/usr/bin/env node
'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const serviceRoot = path.resolve(__dirname, '..');
const controller = fs.readFileSync(path.join(serviceRoot, 'src', 'send.controller.ts'), 'utf8');
const validationLine = controller.split(/\r?\n/).find(line => line.includes('gatewayAdapterVersion format is invalid'));
assert.ok(validationLine, 'send controller must expose adapter-version validation');
const sourceMatch = validationLine.match(/!\/([^/]+)\/\.test\(String\(body\.gatewayAdapterVersion\)\)/);
assert.ok(sourceMatch, 'could not extract adapter-version validator');
const runtimePattern = new RegExp(sourceMatch[1]);
assert.equal(runtimePattern.test('0.13.0+platform.1'), true, 'runtime validator must accept semver build metadata');
assert.equal(runtimePattern.test('0.13.0 platform.1'), false, 'runtime validator must reject whitespace');

const schema = JSON.parse(fs.readFileSync(path.resolve(serviceRoot, '..', '..', 'contracts', 'events', 'gateway-send-command.schema.json'), 'utf8'));
const schemaPattern = new RegExp(schema.properties.gatewayAdapterVersion.pattern);
assert.equal(schemaPattern.test('0.13.0+platform.1'), true, 'JSON schema must accept semver build metadata');
assert.equal(schemaPattern.test('0.13.0 platform.1'), false, 'JSON schema must reject whitespace');

console.log('adapter-version-contract: PASS');
