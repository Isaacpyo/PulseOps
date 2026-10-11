import { act, render, screen } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';

import { markSlow, resetWakeState } from '../lib/wake';
import { WakeBanner } from './WakeBanner';

afterEach(() => resetWakeState());

it('appears while a request is slow and disappears afterwards', () => {
  render(<WakeBanner />);
  expect(screen.queryByRole('status')).not.toBeInTheDocument();

  let done!: () => void;
  act(() => {
    done = markSlow();
  });
  expect(screen.getByRole('status')).toHaveTextContent('Waking the server');

  act(() => done());
  expect(screen.queryByRole('status')).not.toBeInTheDocument();
});
