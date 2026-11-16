import { StatCard } from '@/components/ds';
import { usageNotes, usageTiles } from '@/libs/usage/totals';
import { useUsageI18n } from '@/libs/usage/useUsageI18n';
import { ReactElement } from 'react';

type Props = Parameters<typeof usageTiles>[0];

// The four tiles of a Usage page and, under them, what they do not say.
export default function UsageTiles(props: Props): ReactElement {
  const i18n = useUsageI18n();
  const notes = usageNotes(props.totals, i18n);
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {usageTiles(props, i18n).map((tile) => (
          <StatCard
            key={tile.label}
            label={tile.label}
            value={tile.value}
            note={tile.note}
          />
        ))}
      </div>
      {notes.map((note) => (
        <p key={note} className="text-sm text-muted-foreground">
          {note}
        </p>
      ))}
    </div>
  );
}
