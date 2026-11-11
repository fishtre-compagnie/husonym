import { PageBody, PageHeader } from '@/components/ds';
import { ReactElement, ReactNode } from 'react';

interface Props {
  header: string;
  subHeadings?: ReactNode;
  leftIcon?: ReactNode;
  children: ReactNode;
}

// The page of a new connection of one type: its engine, what it is for, and its form.
export default function NewConnectionLayout(props: Props): ReactElement {
  const { header, subHeadings, leftIcon, children } = props;
  return (
    <PageBody className="max-w-[1320px]">
      <PageHeader
        title={
          <span className="flex min-w-0 items-center gap-3">
            {leftIcon && (
              <span className="grid size-10 shrink-0 place-items-center rounded-[10px] bg-surface-2 [&_svg]:size-6!">
                {leftIcon}
              </span>
            )}
            <span className="truncate">{header}</span>
          </span>
        }
        description={subHeadings}
      />
      {children}
    </PageBody>
  );
}
