import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { OverviewPage } from './OverviewPage';

jest.mock('../api/overview', () => ({
  getOverview: jest.fn().mockResolvedValue({
    held_equities: 5, held_options: 3, option_underlyings: 2, last_option_mark_us: 0,
  }),
}));

test('renders held counts', async () => {
  render(<OverviewPage />);
  await waitFor(() => expect(screen.getByText('5')).toBeInTheDocument());
  expect(screen.getByText('3')).toBeInTheDocument();
});
