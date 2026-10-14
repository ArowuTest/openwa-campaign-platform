import test from 'node:test';
import assert from 'node:assert/strict';

import { availableNavigation, defaultLandingPath, permissionForPath } from '../lib/navigation.ts';

test('availableNavigation exposes only routes backed by the operator permissions', () => {
  const items = availableNavigation(['campaign.read', 'audience.read']);
  assert.deepEqual(items.map((item) => item.href), [
    '/work',
    '/audiences/imports',
    '/audiences/builder',
    '/campaigns'
  ]);
});

test('audit navigation requires audit.read and is available to compliance reviewers', () => {
  assert.equal(availableNavigation(['audit.read']).some((item) => item.href === '/audit'), true);
  assert.equal(availableNavigation(['operations.read']).some((item) => item.href === '/audit'), false);
  assert.equal(permissionForPath('/audit'), 'audit.read');
});

test('user administration is visible only to identity administrators', () => {
  assert.equal(availableNavigation(['identity.admin']).some((item) => item.href === '/admin/users'), true);
  assert.equal(availableNavigation(['campaign.read']).some((item) => item.href === '/admin/users'), false);
  assert.equal(permissionForPath('/admin/users'), 'identity.admin');
  assert.equal(permissionForPath('/admin/users/example'), 'identity.admin');
  assert.equal(defaultLandingPath(['identity.admin']), '/admin/users');
});

test('inbound reply workbench requires dedicated inbound.read even beneath operations', () => {
  assert.equal(permissionForPath('/operations/inbound'), 'inbound.read');
  assert.equal(permissionForPath('/operations/inbound/example'), 'inbound.read');
  assert.equal(availableNavigation(['operations.read']).some((item) => item.href === '/operations/inbound'), false);
  assert.equal(availableNavigation(['inbound.read']).some((item) => item.href === '/operations/inbound'), true);
  assert.equal(defaultLandingPath(['inbound.read']), '/operations/inbound');
});

test('Alert Centre navigation inherits operations.read without operations.write', () => {
  assert.equal(permissionForPath('/operations/alerts'), 'operations.read');
  assert.equal(permissionForPath('/operations/alerts/event'), 'operations.read');
  assert.equal(availableNavigation(['operations.read']).some((item) => item.href === '/operations/alerts'), true);
  assert.equal(availableNavigation(['campaign.read']).some((item) => item.href === '/operations/alerts'), false);
});

test('My Work is accessible with any one source permission, but fails closed without all sources', () => {
  assert.equal(permissionForPath('/work'), 'campaign.read|consent.read|finance.read');
  assert.equal(permissionForPath('/work/finance/campaign-1'), 'finance.read');
  assert.equal(availableNavigation(['campaign.read']).some((item) => item.href === '/work/finance'), false);
  assert.equal(availableNavigation(['finance.read']).some((item) => item.href === '/work'), true);
  assert.equal(availableNavigation(['consent.read']).some((item) => item.href === '/work'), true);
  assert.equal(availableNavigation(['campaign.read']).some((item) => item.href === '/work'), true);
  assert.equal(availableNavigation(['operations.read']).some((item) => item.href === '/work'), false);
  assert.equal(availableNavigation([]).some((item) => item.href === '/work'), false);
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
