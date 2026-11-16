'use client';
import ButtonText from '@/components/ButtonText';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { useGetOnCreateTeamSubmit } from '@/components/site-header/AccountSwitcher';
import { CreateNewTeamDialog } from '@/components/site-header/CreateNewTeamDialog';
import Spinner from '@/components/Spinner';
import { Pill } from '@/components/ds';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { cn } from '@/libs/utils';
import { getErrorMessage, toTitleCase } from '@/util/util';
import { CreateTeamFormValues } from '@/yup-validations/account-switcher';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import { UserAccount, UserAccountService, UserAccountType } from '@husonym/sdk';
import { CheckIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import Error from 'next/error';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { ReactElement, useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';

type PlanName = 'Trial' | 'Team' | 'Enterprise';
type BillingTranslator = ReturnType<typeof useTranslations<'settings.billing'>>;
// The keys of the features under settings.billing.features.
type FeatureKey =
  | 'unlimitedRecords'
  | 'unlimitedJobs'
  | 'oneUser'
  | 'allIntegrations'
  | 'dataAnonymization'
  | 'syntheticData'
  | 'usRegion'
  | 'husonymInfrastructure'
  | 'communitySupport'
  | 'volumePricing'
  | 'unlimitedUsers'
  | 'usOrEuRegion'
  | 'freeTextPii'
  | 'priorityEmailSupport'
  | 'sso'
  | 'rbac'
  | 'webhooks'
  | 'selfHostedOrHusonym'
  | 'auditLogs'
  | 'streamingMode'
  | 'whiteGlove';

interface Plan {
  name: PlanName;
  // The key of the plan's words under settings.billing.plans.
  key: 'trial' | 'team' | 'enterprise';
  features: FeatureKey[];
  planType: UserAccountType;
}

const ALL_PLANS: Plan[] = [
  {
    name: 'Trial',
    key: 'trial',
    features: [
      'unlimitedRecords',
      'unlimitedJobs',
      'oneUser',
      'allIntegrations',
      'dataAnonymization',
      'syntheticData',
      'usRegion',
      'husonymInfrastructure',
      'communitySupport',
    ],
    planType: UserAccountType.PERSONAL,
  },
  {
    name: 'Team',
    key: 'team',
    features: [
      'volumePricing',
      'unlimitedJobs',
      'unlimitedUsers',
      'usOrEuRegion',
      'freeTextPii',
      'priorityEmailSupport',
    ],
    planType: UserAccountType.TEAM,
  },
  {
    name: 'Enterprise',
    key: 'enterprise',
    features: [
      'unlimitedRecords',
      'sso',
      'rbac',
      'webhooks',
      'selfHostedOrHusonym',
      'auditLogs',
      'streamingMode',
      'whiteGlove',
    ],
    planType: UserAccountType.ENTERPRISE,
  },
];

export default function Billing(): ReactElement {
  const { account } = useAccount();
  const { data: systemAppConfigData, isLoading: isSystemAppConfigDataLoading } =
    useGetSystemAppConfig();
  const t = useTranslations('settings.billing');

  if (isSystemAppConfigDataLoading || !account?.type) {
    return <Skeleton />;
  }

  if (!systemAppConfigData?.isHusonymCloud) {
    return <Error statusCode={404} />;
  }

  return (
    <div className="flex flex-col gap-5">
      <SubPageHeader header={t('title')} description={t('description')} />
      <Plans
        account={account}
        upgradeHref={systemAppConfigData?.upgradeLink ?? ''}
        plans={ALL_PLANS}
        isStripeEnabled={systemAppConfigData?.isStripeEnabled ?? false}
      />
    </div>
  );
}

interface ManageSubscriptionProps {
  account: UserAccount;
}

function ManageSubscription(props: ManageSubscriptionProps): ReactElement {
  const { account } = props;

  const { data: isAccountStatusValidResp, isLoading } = useQuery(
    UserAccountService.method.isAccountStatusValid,
    { accountId: account.id },
    { enabled: !!account.id }
  );

  const { mutateAsync: getAccountBillingPortalSessionAsync } = useMutation(
    UserAccountService.method.getAccountBillingPortalSession
  );
  const { mutateAsync: getAccountBillingCheckoutSessionAsync } = useMutation(
    UserAccountService.method.getAccountBillingCheckoutSession
  );
  const router = useRouter();
  const [isGeneratingUrl, setIsGeneratingUrl] = useState(false);
  const t = useTranslations('settings.billing');

  if (account.type === UserAccountType.PERSONAL) {
    return <div />;
  }
  async function onActivateSubscriptionClick(): Promise<void> {
    if (isGeneratingUrl) {
      return;
    }
    setIsGeneratingUrl(true);
    try {
      const resp = await getAccountBillingCheckoutSessionAsync({
        accountId: account.id,
      });
      router.push(resp.checkoutSessionUrl);
    } catch (err) {
      console.error(err);
      toast.error(t('checkoutError'), {
        description: getErrorMessage(err),
      });
    } finally {
      setIsGeneratingUrl(false);
    }
  }
  if (account.hasStripeCustomerId && isLoading) {
    return <Skeleton />;
  }

  if (!account.hasStripeCustomerId || !isAccountStatusValidResp?.isValid) {
    return (
      <div>
        <Button type="button" onClick={() => onActivateSubscriptionClick()}>
          <ButtonText
            leftIcon={isGeneratingUrl ? <Spinner /> : null}
            text={t('activate')}
          />
        </Button>
      </div>
    );
  }

  async function onManageSubscriptionClick(): Promise<void> {
    if (isGeneratingUrl) {
      return;
    }
    setIsGeneratingUrl(true);
    try {
      const resp = await getAccountBillingPortalSessionAsync({
        accountId: account.id,
      });
      router.push(resp.portalSessionUrl);
    } catch (err) {
      console.error(err);
      toast.error(t('portalError'), {
        description: getErrorMessage(err),
      });
    } finally {
      setIsGeneratingUrl(false);
    }
  }

  return (
    <Button type="button" onClick={() => onManageSubscriptionClick()}>
      <ButtonText
        leftIcon={isGeneratingUrl ? <Spinner /> : null}
        text={t('manage')}
      />
    </Button>
  );
}

interface PlansProps {
  account: UserAccount;
  plans: Plan[];
  upgradeHref: string;
  isStripeEnabled: boolean;
}

function Plans({
  account,
  upgradeHref,
  plans,
  isStripeEnabled,
}: PlansProps): ReactElement {
  const t = useTranslations('settings.billing');
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-row gap-3 justify-between items-center ">
        <div className="flex flex-row items-center gap-2 text-sm py-6">
          <p className="font-semibold">{t('currentPlan')}</p>
          <Pill tone="brand">{planPill(account.type, t)}</Pill>
        </div>
        {isStripeEnabled && (
          <div>
            <ManageSubscription account={account} />
          </div>
        )}
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 grid-rows-1 w-full overflow-hidden rounded-2xl border border-line-soft bg-card mt-4 h-full">
        {plans.map((plan, index) => (
          <div
            className={cn(index == 1 && 'bg-surface-2', 'flex flex-col')}
            key={plan.name}
          >
            <div
              className={cn(
                index == 1 && 'md:border-x border-line-soft',
                'h-1/2 px-6 pt-6 flex flex-col justify-between gap-20 w-full'
              )}
            >
              <div className="space-y-2">
                <div className=" items-center flex flex-row gap-4">
                  <div className="font-display text-2xl font-semibold">
                    {t(`plans.${plan.key}.name`)}
                  </div>
                  {plan.name == 'Team' && (
                    <Pill tone="brand">{t('mostPopular')}</Pill>
                  )}
                </div>
                <div className="text-sm text-muted-foreground">
                  {t(`plans.${plan.key}.description`)}
                </div>
              </div>
              <div className="flex flex-col gap-6 w-full">
                <div className="font-display text-2xl md:text-3xl font-semibold tracking-[-0.02em] flex flex-row gap-2 items-end">
                  {t(`plans.${plan.key}.price`)}{' '}
                  {plan.name == 'Team' && (
                    <div className="font-sans text-sm font-normal text-muted-foreground">
                      {t('perMonth')}
                    </div>
                  )}
                </div>
                <div></div>
                <div className={cn(plan.name !== 'Trial' && 'pb-6', 'w-full')}>
                  <PlanButton
                    plan={plan.name}
                    planType={account.type}
                    upgradeHref={upgradeHref}
                    accountSlug={account.name}
                    buttonText={t(`plans.${plan.key}.button`)}
                  />
                  <div className="text-md flex">
                    {plan.name == 'Trial' && t('noCreditCard')}
                  </div>
                </div>
              </div>
            </div>
            <div
              className={cn(
                index == 1 && 'md:border-x border-line-soft',
                'border-t border-line-soft h-1/2 p-6 flex flex-col gap-6'
              )}
            >
              <div className="font-bold">
                {plan.name == 'Team'
                  ? t('everythingInIndividual')
                  : plan.name == 'Enterprise' && t('everythingInTeam')}
              </div>
              <div className="flex flex-col gap-2">
                {plan.features.map((item, index) => (
                  <div key={index}>
                    <div className="flex flex-row items-center gap-2">
                      <CheckIcon className="size-5 shrink-0 text-ok" />
                      <div>{t(`features.${item}`)}</div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function planPill(type: UserAccountType, t: BillingTranslator): string {
  switch (type) {
    case UserAccountType.PERSONAL:
      return t('planPill.personal');
    case UserAccountType.TEAM:
      return t('planPill.team');
    case UserAccountType.ENTERPRISE:
      return t('planPill.enterprise');
    default:
      return t('planPill.other', {
        name: toTitleCase(UserAccountType[type] ?? ''),
      });
  }
}

interface PlanButtonProps {
  plan: string;
  planType: UserAccountType;
  upgradeHref: string;
  accountSlug: string;
  buttonText: string;
}

function PlanButton(props: PlanButtonProps) {
  const { plan, planType, upgradeHref, accountSlug } = props;
  const t = useTranslations('settings.billing');
  switch (plan) {
    case 'Trial':
      if (planType == UserAccountType.PERSONAL) {
        return <GetStartedButton accountSlug={accountSlug} />;
      } else {
        return <div id="empty-personal-button" />;
      }
    case 'Team':
      if (planType == UserAccountType.TEAM) {
        return <GetStartedButton accountSlug={accountSlug} />;
      } else {
        return <CreateNewTeamButton />;
      }
    case 'Enterprise':
      if (planType == UserAccountType.ENTERPRISE) {
        return <GetStartedButton accountSlug={accountSlug} />;
      } else {
        return (
          <Button type="button">
            <Link href={upgradeHref} target="_blank">
              {t('getInTouch')}
            </Link>
          </Button>
        );
      }
  }
}

interface GetStartedButtonProps {
  accountSlug: string;
}

function GetStartedButton(props: GetStartedButtonProps): ReactElement {
  const { accountSlug } = props;
  const t = useTranslations('settings.billing');
  return (
    <Button type="button" className="w-full">
      <Link href={`/${accountSlug}/new/job`}>{t('getStarted')}</Link>
    </Button>
  );
}

function CreateNewTeamButton(): ReactElement {
  const form = useForm<CreateTeamFormValues>({
    mode: 'onChange',
    resolver: yupResolver(CreateTeamFormValues),
    defaultValues: {
      name: '',
      convertPersonalToTeam: false,
    },
  });
  const [showNewTeamDialog, setShowNewTeamDialog] = useState(false);
  const { account } = useAccount();
  const t = useTranslations('settings.billing');

  const onSubmit = useGetOnCreateTeamSubmit({
    onDone() {
      setShowNewTeamDialog(false);
    },
  });

  return (
    <CreateNewTeamDialog
      form={form}
      open={showNewTeamDialog}
      onOpenChange={(val) => {
        setShowNewTeamDialog(val);
        form.reset();
      }}
      onSubmit={onSubmit}
      onCancel={() => {
        setShowNewTeamDialog(false);
        form.reset();
      }}
      trigger={
        <Button className="w-full" onClick={() => setShowNewTeamDialog(true)}>
          {t('createTeam')}
        </Button>
      }
      showSubscriptionInfo={true} // This is only rendered in husonym cloud
      showConvertPersonalToTeamOption={
        account?.type === UserAccountType.PERSONAL
      }
    />
  );
}
