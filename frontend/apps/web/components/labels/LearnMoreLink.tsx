import { cn } from '@/libs/utils';
import { ArrowTopRightIcon } from '@radix-ui/react-icons';
import Link from 'next/link';
import { ReactElement } from 'react';

interface Props {
  href: string;
  classNames?: string;
}

export default function LearnMoreLink(props: Props): ReactElement {
  const { href, classNames } = props;

  return (
    <Link
      href={href}
      target="_blank"
      className={cn(
        'inline-flex items-center gap-1 text-sm text-link underline-offset-2 hover:underline',
        classNames
      )}
    >
      <span>Learn more</span>
      <ArrowTopRightIcon className="w-3 h-3" />
    </Link>
  );
}
