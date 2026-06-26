import React, { useEffect, useState } from 'react';
import { Stack } from '@grafana/ui';

import { Page } from '../components/Page';
import { Section } from '../components/Section';
import { getOverview, type Overview } from '../api/overview';

function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <Stack direction="column" gap={0.5}>
      <span style={{ fontSize: 28, fontWeight: 600 }}>{value}</span>
      <span style={{ opacity: 0.7 }}>{label}</span>
    </Stack>
  );
}

export function OverviewPage() {
  const [o, setO] = useState<Overview | null>(null);
  useEffect(() => {
    getOverview().then(setO);
  }, []);
  const lastMark = o && o.last_option_mark_us > 0 ? new Date(o.last_option_mark_us / 1000).toLocaleString() : '—';
  return (
    <Page>
      <Page.Contents>
        <h2>Overview</h2>
        <Section title="Holdings" description="Instruments discovered from imported portfolios">
          <Stack direction="row" gap={4}>
            <Stat label="Equities" value={o?.held_equities ?? '—'} />
            <Stat label="Options" value={o?.held_options ?? '—'} />
            <Stat label="Option underlyings" value={o?.option_underlyings ?? '—'} />
          </Stack>
        </Section>
        <Section title="Option marks" description="Latest Yahoo option-mark poll">
          <Stat label="Last option mark" value={lastMark} />
        </Section>
      </Page.Contents>
    </Page>
  );
}
