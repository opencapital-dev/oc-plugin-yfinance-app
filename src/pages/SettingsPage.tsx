import React, { useEffect, useState } from 'react';
import { Alert, Button, Field, Input, SecretInput, Switch } from '@grafana/ui';

import { Page } from '../components/Page';
import { Section } from '../components/Section';
import { getSettings, putSettings, testFred, type Settings } from '../api/settings';

export function SettingsPage() {
  const [s, setS] = useState<Settings | null>(null);
  const [key, setKey] = useState('');
  const [msg, setMsg] = useState<string | null>(null);

  useEffect(() => {
    getSettings().then(setS);
  }, []);

  const saveFred = async () => {
    await putSettings({ fred_api_key: key });
    setMsg('Saved');
    setKey('');
    getSettings().then(setS);
  };

  const savePoll = async (patch: Partial<Settings>) => {
    await putSettings(patch);
    setMsg('Saved');
    getSettings().then(setS);
  };

  const test = async () => {
    const r = await testFred();
    setMsg(r.ok ? 'FRED key OK' : 'FRED key invalid');
  };

  return (
    <Page>
      <Page.Contents>
        <h2>Settings</h2>

        <Section title="API keys">
          <Field label="FRED API key" description="Get one at fredaccount.stlouisfed.org. Stored locally.">
            <SecretInput
              isConfigured={!!s?.fred_api_key_set}
              value={key}
              placeholder={s?.fred_api_key_set ? 'configured' : 'enter key'}
              onChange={(e) => setKey(e.currentTarget.value)}
              onReset={() => setKey('')}
            />
          </Field>
          <div>
            <Button onClick={saveFred}>Save</Button>{' '}
            <Button variant="secondary" onClick={test}>
              Test
            </Button>
          </div>
        </Section>

        <Section title="Option polling" description="Yahoo option-chain marks for held option positions.">
          <Field label="Enabled" description="Poll held option chains and publish marks.">
            <Switch
              value={s?.optionPollEnable ?? true}
              onChange={() => savePoll({ optionPollEnable: !(s?.optionPollEnable ?? true) })}
            />
          </Field>
          <Field label="Interval (seconds)" description="How often to poll (default 900 = 15 min).">
            <Input
              type="number"
              width={20}
              value={s?.optionPollIntervalSec ?? 900}
              onChange={(e) => {
                const v = Number(e.currentTarget.value);
                if (!isNaN(v) && v >= 1) {
                  setS(s ? { ...s, optionPollIntervalSec: v } : s);
                }
              }}
              onBlur={() => {
                if (s && !isNaN(s.optionPollIntervalSec) && s.optionPollIntervalSec >= 1) {
                  savePoll({ optionPollIntervalSec: s.optionPollIntervalSec });
                }
              }}
            />
          </Field>
        </Section>

        {msg && <Alert title={msg} severity="info" />}
      </Page.Contents>
    </Page>
  );
}
