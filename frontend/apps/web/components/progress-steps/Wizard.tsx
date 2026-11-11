/**
 * The building blocks of a focused, step-by-step flow (the job creation
 * wizard): a top bar with a way out and the step indicator, a heading that
 * says where you are, selectable cards, and a sticky footer holding Back and
 * Continue. Colors come from the design tokens so both themes follow.
 */
import { Eyebrow } from '@/components/ds';
import { cn } from '@/libs/utils';
import { Cross2Icon } from '@radix-ui/react-icons';
import Image from 'next/image';
import Link from 'next/link';
import {
  ComponentPropsWithoutRef,
  ReactElement,
  ReactNode,
  forwardRef,
} from 'react';

/** The page of a wizard: its top bar, then the current step's content. */
export function WizardFrame(props: {
  title: ReactNode;
  exitHref: string;
  exitLabel: string;
  steps?: ReactNode;
  /** Tables and mapping grids get the full page width. */
  wide?: boolean;
  children: ReactNode;
  id?: string;
}): ReactElement {
  const { title, exitHref, exitLabel, steps, wide, children, id } = props;
  return (
    <div id={id} className="flex min-h-full w-full flex-col">
      <div className="border-b border-line-soft">
        <div
          className={cn(
            'mx-auto flex w-full flex-wrap items-center gap-x-4 gap-y-3 px-4 py-3.5 sm:px-6',
            wide ? 'max-w-[1600px]' : 'max-w-[1120px]'
          )}
        >
          <Link
            href={exitHref}
            aria-label={exitLabel}
            title={exitLabel}
            className="grid size-[38px] shrink-0 place-items-center rounded-[10px] border border-border text-foreground transition-colors hover:bg-hover"
          >
            <Cross2Icon className="size-4" aria-hidden="true" />
          </Link>
          <span className="font-display text-[17px] font-semibold">
            {title}
          </span>
          {steps && <div className="min-w-0 sm:ml-auto">{steps}</div>}
        </div>
      </div>
      <div
        className={cn(
          'mx-auto flex w-full flex-1 flex-col px-4 pt-9 sm:px-6',
          wide ? 'max-w-[1600px]' : 'max-w-[1120px]'
        )}
      >
        <div className="flex flex-1 animate-step-in flex-col gap-7">
          {children}
        </div>
      </div>
    </div>
  );
}

/** The step's position, its question, and what it means. */
export function WizardHeading(props: {
  position?: { index: number; total: number };
  title: ReactNode;
  description?: ReactNode;
  mascot?: boolean;
  actions?: ReactNode;
}): ReactElement {
  const { position, title, description, mascot, actions } = props;
  return (
    <div className="flex flex-wrap items-center gap-5">
      <div className="min-w-0 flex-[1_1_400px]">
        {position && (
          <Eyebrow className="text-xs">
            Step {position.index} of {position.total}
          </Eyebrow>
        )}
        <h1 className="mb-0 mt-1.5 font-display text-[32px] font-semibold leading-tight tracking-[-0.02em]">
          {title}
        </h1>
        {description && (
          <p className="mb-0 mt-2 max-w-[68ch] leading-normal text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
      {mascot && (
        <Image
          src="/images/mascot.png"
          alt=""
          width={92}
          height={92}
          className="h-auto w-[92px] shrink-0"
        />
      )}
    </div>
  );
}

/** A group of the step: a label, an optional line beneath, its content. */
export function WizardSection(props: {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
  id?: string;
}): ReactElement {
  const { title, description, children, className, id } = props;
  return (
    <section
      aria-labelledby={id}
      className={cn('flex flex-col gap-3', className)}
    >
      <div className="flex flex-col gap-0.5">
        <h2 id={id} className="m-0 text-[15px] font-semibold">
          {title}
        </h2>
        {description && (
          <p className="m-0 text-[13px] leading-normal text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {children}
    </section>
  );
}

/** A surface for a block of settings, as in the mockups' option panels. */
export function WizardPanel(
  props: ComponentPropsWithoutRef<'div'> & { title?: ReactNode }
): ReactElement {
  const { className, title, children, ...rest } = props;
  return (
    <div
      className={cn(
        'overflow-hidden rounded-[14px] border border-line-soft bg-card',
        className
      )}
      {...rest}
    >
      {title && (
        <div className="border-b border-line-soft px-[18px] py-4 font-semibold">
          {title}
        </div>
      )}
      <div className="p-[18px]">{children}</div>
    </div>
  );
}

/** The classes of a selectable card, so a card can also be a link. */
function selectCardClass(selected: boolean, disabled?: boolean): string {
  return cn(
    'flex rounded-[14px] border text-left text-foreground transition-[border-color,box-shadow] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-link',
    selected
      ? 'border-primary bg-brand-soft shadow-[0_0_0_3px_var(--brand-soft)]'
      : 'border-border bg-card',
    disabled
      ? 'cursor-not-allowed opacity-55'
      : 'cursor-pointer hover:border-quiet'
  );
}

/**
 * A card picked among others. It is a radio by default; pass `role` to make
 * it a checkbox. Put several in an element with role="radiogroup".
 */
export const SelectCard = forwardRef<
  HTMLButtonElement,
  Omit<ComponentPropsWithoutRef<'button'>, 'role'> & {
    selected: boolean;
    role?: 'radio' | 'checkbox';
  }
>(function SelectCard(props, ref) {
  const { selected, role = 'radio', className, disabled, ...rest } = props;
  return (
    <button
      ref={ref}
      type="button"
      role={role}
      aria-checked={selected}
      disabled={disabled}
      className={cn(selectCardClass(selected, disabled), className)}
      {...rest}
    />
  );
});

/** A short label on the surface tint, for the footer's recap. */
function WizardChip(props: {
  children: ReactNode;
  className?: string;
}): ReactElement {
  return (
    <span
      className={cn(
        'max-w-full truncate rounded-full bg-surface-2 px-2.5 py-1 text-xs text-foreground',
        props.className
      )}
    >
      {props.children}
    </span>
  );
}

/**
 * The bar that stays at the bottom of the screen: a recap of what is chosen
 * so far on the left, then Back and Continue.
 */
export function WizardFooter(props: {
  summary?: ReactNode[];
  children: ReactNode;
  className?: string;
}): ReactElement {
  const { summary, children, className } = props;
  const chips = (summary ?? []).filter((s) => !!s);
  return (
    <div
      className={cn(
        'sticky bottom-0 z-10 -mx-4 mt-auto border-t border-line-soft bg-header px-4 py-3.5 backdrop-blur-md sm:-mx-6 sm:px-6',
        className
      )}
    >
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-[1_1_260px] flex-wrap gap-1.5">
          {chips.map((chip, i) => (
            <WizardChip key={i}>{chip}</WizardChip>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">{children}</div>
      </div>
    </div>
  );
}
