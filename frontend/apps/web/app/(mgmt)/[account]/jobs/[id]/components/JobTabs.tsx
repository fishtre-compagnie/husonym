'use client';
import { cn } from '@/libs/utils';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { ReactElement } from 'react';

export interface JobTab {
  title: string;
  href: string;
  // A count beside the title: the changes waiting for a review.
  count?: number;
}

interface Props {
  items: JobTab[];
}

// The sections of a job, as a bar of tabs under its header. Each is a page of its
// own, so they are links: the current one is marked with aria-current.
export default function JobTabs({ items }: Props): ReactElement {
  const pathname = usePathname();
  return (
    <nav
      aria-label="Job sections"
      className="-mb-px flex gap-1 overflow-x-auto border-b border-line-soft"
    >
      {items.map((item) => {
        const current = pathname === item.href;
        return (
          <Link
            key={item.href}
            href={item.href}
            aria-current={current ? 'page' : undefined}
            className={cn(
              'flex shrink-0 items-center gap-2 whitespace-nowrap border-b-2 px-3 py-2.5 text-sm transition-colors',
              current
                ? 'border-primary font-medium text-hi'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            )}
          >
            {item.title}
            {item.count !== undefined && item.count > 0 && (
              <span className="rounded-full bg-danger-soft px-1.5 py-px text-xs font-medium text-danger-fg tabular-nums">
                {item.count}
              </span>
            )}
          </Link>
        );
      })}
    </nav>
  );
}
