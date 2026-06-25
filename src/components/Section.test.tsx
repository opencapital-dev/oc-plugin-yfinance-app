import React from 'react';
import { render, screen } from '@testing-library/react';
import { Section } from './Section';

test('renders title, description, and children', () => {
  render(
    <Section title="Option polling" description="Controls the poll loop">
      <div>child-content</div>
    </Section>,
  );
  expect(screen.getByText('Option polling')).toBeInTheDocument();
  expect(screen.getByText('Controls the poll loop')).toBeInTheDocument();
  expect(screen.getByText('child-content')).toBeInTheDocument();
});
