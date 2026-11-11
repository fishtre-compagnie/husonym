import { Skeleton } from '@/components/ui/skeleton';
import { ReactElement } from 'react';

export default function JobIdSkeletonForm(): ReactElement {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <Skeleton className="h-9 w-64" />
        <div className="flex flex-wrap items-center gap-2">
          <Skeleton className="h-10 w-28" />
          <Skeleton className="h-10 w-28" />
          <Skeleton className="h-10 w-32" />
        </div>
      </div>
      <Skeleton className="h-10 w-full" />
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[3fr_1fr]">
        <Skeleton className="h-40 rounded-2xl" />
        <Skeleton className="h-40 rounded-2xl" />
      </div>
      <Skeleton className="h-48 w-full rounded-2xl" />
    </div>
  );
}
