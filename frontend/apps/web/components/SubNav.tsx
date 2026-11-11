import { cn } from '@/libs/utils';
import Link from 'next/link';
import { usePathname } from 'next/navigation';

interface SubNavProps extends React.HTMLAttributes<HTMLElement> {
  items: {
    href: string;
    title: string;
  }[];
  buttonClassName?: string;
}

export function SubNav({
  className,
  items,
  buttonClassName,
  ...props
}: SubNavProps) {
  const pathname = usePathname();
  return (
    <nav
      className={cn(
        'flex w-fit max-w-full flex-row flex-wrap gap-1 rounded-[10px] border border-border bg-card p-[3px]',
        className
      )}
      {...props}
    >
      {items.map((item) => {
        return (
          <Link
            key={item.href}
            href={item.href}
            aria-current={pathname === item.href ? 'page' : undefined}
            className={cn(
              'inline-flex h-8 items-center rounded-[7px] px-3 text-sm font-medium transition-colors',
              pathname === item.href
                ? 'bg-surface-2 text-hi'
                : 'text-muted-foreground hover:bg-hover hover:text-foreground',
              buttonClassName
            )}
          >
            {item.title}
          </Link>
        );
      })}
    </nav>
  );
}
