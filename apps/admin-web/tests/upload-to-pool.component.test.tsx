import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { ImportCohorts } from '../app/audiences/imports/import-cohorts';
import { DeliveryMonitor, FrozenAudiencePicker } from '../app/campaigns/[id]/sending-controls';
import { envelope, mockHTTP } from './audience-component-test-helpers';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
const source = { id: 'aaaaaaaa-0000-4000-8000-000000000001', organisationId: 'org-a', sourceName: 'November list' };

test('resumes a partial split, reuses the exact saved group and rejects a different split', async () => {
  const inventory: object[] = [];
  let attempts = 0;
  mockHTTP((path, init) => {
    if (path.startsWith('/v1/segments?organisationId=org-a')) return envelope(inventory);
    if (path === '/v1/segments' && init.method === 'POST') {
      attempts++;
      if (attempts === 2) throw new Error('interrupted');
      const body = JSON.parse(String(init.body));
      const segment = { ...body, id: 'segment-' + attempts, status: 'ACTIVE' };
      inventory.push(segment);
      return segment;
    }
    throw new Error('Unexpected request ' + path);
  });
  render(<ImportCohorts source={source} />);
  const count = screen.getByLabelText('Number of groups');
  const prepare = screen.getByRole('button', { name: 'Prepare groups' });
  fireEvent.change(count, { target: { value: '2' } });
  fireEvent.click(prepare);
  await screen.findByText(/Confirmed 1 of 2/);
  fireEvent.click(prepare);
  await screen.findByText(/All 2 groups are saved/);
  expect(attempts).toBe(3);
  expect(inventory).toEqual([1,2].map(i => expect.objectContaining({organisationId:'org-a',definition:{
    join:'AND',rules:[{definitionCode:'IMPORT_COHORT',operator:'equals',values:[source.id + '/' + i + '/2']}]
  }})));
  fireEvent.change(count, { target: { value: '3' } });
  fireEvent.click(prepare);
  await screen.findByText(/different split/);
  expect(attempts).toBe(3);
});

test('only a completed snapshot from this campaign can advance to validation', async () => {
  mockHTTP(() => envelope([
    {campaignId:'a',snapshotId:'right',status:'COMPLETED',processedCount:8},
    {campaignId:'b',snapshotId:'wrong',status:'COMPLETED',processedCount:5},
    {campaignId:'a',snapshotId:'pending',status:'RUNNING',processedCount:1}
  ]));
  const validate = vi.fn(async () => {});
  render(<FrozenAudiencePicker campaignId="a" maximum={10} busy={false} validate={validate}/>);
  await screen.findByRole('option', {name:/right/});
  expect(screen.queryByRole('option',{name:/wrong|pending/})).toBeNull();
  fireEvent.change(screen.getByLabelText('Completed campaign snapshot'),{target:{value:'right'}});
  fireEvent.click(screen.getByRole('button',{name:'Validate selected audience'}));
  expect(validate).toHaveBeenCalledWith('right');
});

test('monitor refreshes only its campaign and distinguishes failed reads from zero delivery', async () => {
  let fails = false;
  mockHTTP(path => {
    if (path !== '/v1/campaigns/a/metrics' || fails) throw new Error('Unavailable');
    return {sentTotal:7,deliveredTotal:0};
  });
  render(<DeliveryMonitor campaignId="a" pool="pool-a" status="COMPLETED"/>);
  const refresh = screen.getByRole('button',{name:'Refresh delivery counts'});
  fireEvent.click(refresh);
  await screen.findByText('7');
  expect(within(screen.getByText('Delivered').parentElement!).getByText('0')).toBeTruthy();
  fails = true;
  fireEvent.click(refresh);
  await waitFor(() => expect(screen.queryByText('7')).toBeNull());
  expect(within(screen.getByText('Delivered').parentElement!).getByText('Unavailable')).toBeTruthy();
});
