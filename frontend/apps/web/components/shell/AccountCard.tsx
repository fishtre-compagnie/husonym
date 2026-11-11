import { cn } from '@/libs/utils';
import { UserAccount, UserAccountType } from '@husonym/sdk';
import { ComponentPropsWithRef, ReactElement } from 'react';
import { ChevronDownIcon } from './icons';

function accountKind(type: UserAccountType | undefined): string | undefined {
  switch (type) {
    case UserAccountType.PERSONAL:
      return 'Personal account';
    case UserAccountType.TEAM:
      return 'Team account';
    case UserAccountType.ENTERPRISE:
      return 'Enterprise account';
    default:
      return undefined;
  }
}

type Props = ComponentPropsWithRef<'button'> & {
  account: UserAccount | undefined;
  // A button that opens the switcher, with a chevron; otherwise a plain card.
  interactive?: boolean;
};

/** The active account, as the sidebar shows it: its initial, its name, its kind. */
export function AccountCard(props: Props): ReactElement {
  const { account, interactive, className, ...rest } = props;
  const kind = accountKind(account?.type);
  const inner = (
    <>
      <span
        aria-hidden="true"
        className="grid size-6 shrink-0 place-items-center rounded-md bg-primary font-display text-xs font-bold text-primary-foreground"
      >
        {(account?.name ?? '?').charAt(0).toUpperCase()}
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate font-semibold text-foreground">
          {account?.name ?? '…'}
        </span>
        {kind && (
          <span className="truncate text-xs text-muted-foreground">{kind}</span>
        )}
      </span>
      {interactive && (
        <ChevronDownIcon className="shrink-0 text-muted-foreground" />
      )}
    </>
  );
  const cardClass = cn(
    'flex w-full items-center gap-2.5 rounded-[10px] border border-border bg-card px-3 py-2.5 text-left text-sm',
    interactive && 'cursor-pointer transition-colors hover:bg-hover',
    className
  );
  if (!interactive) {
    return <div className={cardClass}>{inner}</div>;
  }
  return (
    <button type="button" className={cardClass} {...rest}>
      {inner}
    </button>
  );
}
