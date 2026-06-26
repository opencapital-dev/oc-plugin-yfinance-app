import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { SettingsPage } from './SettingsPage';

jest.mock('../api/settings', () => ({
  getSettings: jest.fn().mockResolvedValue({
    fred_api_key_set: true, pollIntervalSec: 15, qps: 1, burst: 3,
    liveEnable: true, backfillEnable: true,
    optionPollEnable: true, optionPollIntervalSec: 900,
  }),
  putSettings: jest.fn().mockResolvedValue({ ok: true }),
  testFred: jest.fn(),
}));

test('renders Option polling section with interval', async () => {
  render(<SettingsPage />);
  await waitFor(() => expect(screen.getByText('Option polling')).toBeInTheDocument());
  expect(screen.getByDisplayValue('900')).toBeInTheDocument();
});
