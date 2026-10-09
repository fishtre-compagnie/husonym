import { Skeleton } from '@/components/ui/skeleton';
import { ReactElement } from 'react';

interface Props {
  // The height of each card under the tiles, as a class, from top to bottom.
  cards: readonly string[];
}

// The shape of a Usage page while it is read: the four tiles, then a card each.
export default function UsageSkeleton(props: Props): ReactElement {
  return (
    <>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-24 w-full" />
        ))}
      </div>
      {props.cards.map((height, i) => (
        <Skeleton key={i} className={`${height} w-full`} />
      ))}
    </>
  );
}
