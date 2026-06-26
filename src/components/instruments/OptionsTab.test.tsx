import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { OptionsTab } from './OptionsTab';

jest.mock('../../api/options', () => ({
  listOptionUnderlyings: jest.fn().mockResolvedValue([
    { root: 'AAPL', portfolio_id: 'pf1', symbol: 'AAPL', subscribed: true, held_contracts: 2 },
  ]),
  setOptionUnderlyingSymbol: jest.fn(),
  toggleOptionUnderlying: jest.fn(),
}));

test('lists option underlyings with held count', async () => {
  render(<OptionsTab />);
  await waitFor(() => expect(screen.getByText('AAPL')).toBeInTheDocument());
  expect(screen.getByText('2')).toBeInTheDocument();
});
