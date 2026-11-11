import { Panel } from '@/components/ds';
import { ReactElement } from 'react';

interface Props {
  label: string;
  value: number | string;
  // A color for the value, when the value has a meaning a color can carry.
  accent?: string;
  // A line under the label: what the value does not say by itself.
  note?: string;
}

// A number and what it counts: the look of the design system's StatCard, label on top.
export default function StatTile(props: Props): ReactElement {
  const { label, value, accent, note } = props;
  return (
    <Panel className="flex flex-col gap-2.5 p-5">
      <span className="text-[13px] text-muted-foreground">{label}</span>
      <span
        className="font-display text-[32px] font-semibold leading-none tracking-[-0.02em] tabular-nums"
        style={accent ? { color: accent } : undefined}
      >
        {value}
      </span>
      {note && (
        <span className="text-[13px] text-muted-foreground">{note}</span>
      )}
    </Panel>
  );
}
