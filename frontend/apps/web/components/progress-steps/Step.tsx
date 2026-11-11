import { cn } from '@/libs/utils';
import { toTitleCase } from '@/util/util';
import { CheckIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';

interface Props {
  isCompleted: boolean;
  isActive: boolean;
  isLastStep: boolean;
  name: string;
  /** What the step reads as; defaults to the name in title case. */
  label?: string;
  /** The 1-based position shown in the dot while the step is not done. */
  position?: number;
}

/**
 * One step of a wizard's indicator: a pill with a numbered dot, a check once
 * the step is done, and the brand tint on the current one.
 */
export default function Step(props: Props): ReactElement {
  const { isActive, isCompleted, name, label, position } = props;

  return (
    <li>
      <span
        aria-current={isActive ? 'step' : undefined}
        className={cn(
          'flex items-center gap-2 rounded-full border py-1.5 pl-[7px] pr-[11px] text-[13px] transition-colors',
          isActive
            ? 'border-primary bg-brand-soft text-foreground'
            : 'border-border bg-transparent',
          !isActive &&
            (isCompleted ? 'text-foreground' : 'text-muted-foreground')
        )}
      >
        <span
          aria-hidden="true"
          className={cn(
            'grid size-[22px] place-items-center rounded-full text-xs font-semibold',
            isActive
              ? 'bg-primary text-primary-foreground'
              : isCompleted
                ? 'bg-ok-soft text-ok'
                : 'bg-surface-2 text-muted-foreground'
          )}
        >
          {isCompleted ? <CheckIcon className="size-3.5" /> : position}
        </span>
        {label ?? toTitleCase(name)}
        {isCompleted && <span className="sr-only">(done)</span>}
      </span>
    </li>
  );
}
