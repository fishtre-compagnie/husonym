import { Card, CardContent } from '@/components/ui/card';
import { ReactElement } from 'react';

interface Props {
  label: string;
  value: number | string;
  // A color for the value, when the value has a meaning a color can carry.
  accent?: string;
  // A line under the label: what the value does not say by itself.
  note?: string;
}

// A number and what it counts.
export default function StatTile(props: Props): ReactElement {
  const { label, value, accent, note } = props;
  return (
    <Card className="transition-shadow hover:shadow-md">
      <CardContent className="flex flex-col gap-1 pt-6">
        <span
          className="text-3xl font-semibold tabular-nums"
          style={accent ? { color: accent } : undefined}
        >
          {value}
        </span>
        <span className="text-sm text-muted-foreground">{label}</span>
        {note && <span className="text-xs text-muted-foreground">{note}</span>}
      </CardContent>
    </Card>
  );
}
