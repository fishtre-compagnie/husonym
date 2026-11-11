'use client';
import { SystemAppConfig } from '@/app/config/app-config';
import { StatusDot } from '@/components/ds';
import { cn } from '@/libs/utils';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useQuery } from '@connectrpc/connect-query';
import { AccountStatus, UserAccountService } from '@husonym/sdk';
import { differenceInDays } from 'date-fns';
import { useAccount } from '../providers/account-provider';
import { Skeleton } from '../ui/skeleton';
import Upgrade from './Upgrade';

interface Props {
  systemAppConfig: SystemAppConfig;
}

export function AccountStatusHandler(props: Props) {
  const { systemAppConfig } = props;
  const { account } = useAccount();

  const { data: data, isLoading } = useQuery(
    UserAccountService.method.isAccountStatusValid,
    { accountId: account?.id },
    { enabled: !!account?.id }
  );

  if (isLoading) {
    return <Skeleton className="h-10 w-[100px] rounded-[10px]" />;
  }

  const showTrialCountdown =
    systemAppConfig.isHusonymCloud &&
    (data?.accountStatus == AccountStatus.ACCOUNT_TRIAL_ACTIVE ||
      data?.accountStatus == AccountStatus.ACCOUNT_TRIAL_EXPIRED);

  const trialEndDate = data?.trialExpiresAt
    ? timestampDate(data.trialExpiresAt)
    : new Date();

  const daysRemaining = Math.max(differenceInDays(trialEndDate, new Date()));

  return (
    <div className="flex flex-row items-center gap-2">
      {showTrialCountdown && (
        <TrialCountdown
          isExpired={data?.accountStatus == AccountStatus.ACCOUNT_TRIAL_EXPIRED}
          isAlmostExpired={daysRemaining <= 3}
          daysRemaining={daysRemaining}
        />
      )}
      <Upgrade
        upgradeLink={systemAppConfig.upgradeLink}
        isAccountStatusValidResp={data}
        isLoading={isLoading}
      />
    </div>
  );
}

interface TrialCountdownProps {
  isExpired: boolean;
  isAlmostExpired: boolean;
  daysRemaining: number;
}

function TrialCountdown(props: TrialCountdownProps) {
  const { isExpired, isAlmostExpired, daysRemaining } = props;
  const tone = isExpired ? 'danger' : isAlmostExpired ? 'warn' : 'brand';

  return (
    <div
      className={cn(
        'flex h-10 items-center gap-2 rounded-[10px] border px-3',
        tone === 'danger' && 'border-danger-line bg-danger-soft text-danger-fg',
        tone === 'warn' && 'border-warn-line bg-warn-soft text-warn-fg',
        tone === 'brand' && 'border-border bg-brand-soft text-foreground'
      )}
    >
      <StatusDot
        tone={tone}
        pulse
        className={tone === 'brand' ? 'bg-link' : undefined}
      />
      <div className="text-xs font-medium whitespace-nowrap">
        {isExpired
          ? 'Trial Expired'
          : `${daysRemaining} day${daysRemaining !== 1 ? 's' : ''} left in your trial`}
      </div>
    </div>
  );
}
