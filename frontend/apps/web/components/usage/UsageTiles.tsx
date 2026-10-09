import StatTile from '@/components/StatTile';
import { usageNotes, usageTiles } from '@/libs/usage/totals';
import { ReactElement } from 'react';

type Props = Parameters<typeof usageTiles>[0];

// The four tiles of a Usage page and, under them, what they do not say.
export default function UsageTiles(props: Props): ReactElement {
  const notes = usageNotes(props.totals);
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {usageTiles(props).map((tile) => (
          <StatTile
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
