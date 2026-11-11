import { cn } from '@/libs/utils';

export function Skeleton({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('animate-pulse rounded-lg bg-surface-2', className)}
      {...props}
    />
  );
}
