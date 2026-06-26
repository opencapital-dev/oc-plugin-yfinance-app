import React, { type ReactNode } from 'react';
import { css } from '@emotion/css';
import { type GrafanaTheme2 } from '@grafana/data';
import { useStyles2 } from '@grafana/ui';

export function Section({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  const s = useStyles2(getStyles);
  return (
    <section className={s.card}>
      <header className={s.header}>
        <h3 className={s.title}>{title}</h3>
        {description && <p className={s.desc}>{description}</p>}
      </header>
      <div className={s.body}>{children}</div>
    </section>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  card: css({
    background: theme.colors.background.secondary,
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    padding: theme.spacing(3),
    marginBottom: theme.spacing(3),
  }),
  header: css({ marginBottom: theme.spacing(2) }),
  title: css({ margin: 0, fontSize: theme.typography.h4.fontSize }),
  desc: css({ margin: theme.spacing(0.5, 0, 0), color: theme.colors.text.secondary, fontSize: theme.typography.bodySmall.fontSize }),
  body: css({ display: 'flex', flexDirection: 'column', gap: theme.spacing(2) }),
});
