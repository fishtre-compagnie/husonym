import { cn } from '@/libs/utils';
import { ReactNode, type JSX } from 'react';
import { Badge } from '../ui/badge';

interface Props {
  header: string;
  leftBadgeValue?: string;
  extraHeading?: ReactNode;
  leftIcon?: ReactNode;
  progressSteps?: JSX.Element;
  pageHeaderContainerClassName?: string;
  subHeadings?: ReactNode | ReactNode[];
}

export default function PageHeader(props: Props) {
  const {
    header,
    extraHeading,
    leftIcon,
    pageHeaderContainerClassName,
    leftBadgeValue,
    progressSteps,
    subHeadings: subHeadingsOrSingle,
  } = props;
  const subHeadings = Array.isArray(subHeadingsOrSingle)
    ? subHeadingsOrSingle
    : [subHeadingsOrSingle];
  return (
    <div
      className={cn(
        'page-header-container flex flex-col gap-2',
        pageHeaderContainerClassName
      )}
    >
      <div className="flex flex-col xl:flex-row xl:items-center items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col items-start gap-3 md:flex-row md:items-center">
          {leftIcon ? leftIcon : null}
          <div className="flex min-w-0 flex-row flex-wrap items-center gap-3">
            <h1 className="m-0 max-w-full truncate font-display text-[26px] font-semibold leading-tight tracking-[-0.02em] text-hi sm:text-[30px] lg:max-w-[720px]">
              {header}
            </h1>
            {leftBadgeValue && (
              <Badge variant="outline" className="text-nowrap">
                {leftBadgeValue}
              </Badge>
            )}
          </div>
        </div>
        {progressSteps && (
          <div className="flex-1 flex justify-center">{progressSteps}</div>
        )}
        {extraHeading ? <div>{extraHeading}</div> : null}
      </div>
      {subHeadings.map((subheading, ind) => (
        <div key={ind} className="text-sm text-muted-foreground">
          {subheading}
        </div>
      ))}
    </div>
  );
}
