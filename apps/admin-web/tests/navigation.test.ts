import test from 'node:test';
import assert from 'node:assert/strict';

import { availableNavigation, defaultLandingPath, permissionForPath } from '../lib/navigation.ts';

test('availableNavigation exposes only routes backed by the operator permissions', () => {
  const items = availableNavigation(['campaign.read', 'audience.read']);
  assert.deepEqual(items.map((item) => item.href), [
    '/audiences/imports',
    '/audiences/builder',
    '/campaigns'
  ]);
});

test('wildcard receives the complete internal navigation', () => {
  const items = availableNavigation(['*']);
  assert.ok(items.length >= 9);
  assert.ok(items.some((item) => item.href === '/operations'));
  assert.ok(items.some((item) => item.href === '/reports'));
});

test('defaultLandingPath never chooses a route the user cannot read', () => {
  assert.equal(defaultLandingPath(['operations.read']), '/');
  assert.equal(defaultLandingPath(['campaign.read']), '/campaigns');
  assert.equal(defaultLandingPath(['sender.read']), '/senders');
  assert.equal(defaultLandingPath([]), '/access-denied');
});

test('permissionForPath uses the most specific protected route and leaves auth routes public', () => {
  assert.equal(permissionForPath('/campaigns/abc'), 'campaign.read');
  assert.equal(permissionForPath('/audiences/imports/import-1'), 'audience.read');
  assert.equal(permissionForPath('/login'), undefined);
  assert.equal(permissionForPath('/step-up'), undefined);
});
