'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { getErrorMessage } from '@/util/util';
import { TemporalFormValues } from '@/yup-validations/temporal';
import { create } from '@bufbuild/protobuf';
import {
  createConnectQueryKey,
  useMutation,
  useQuery,
} from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  AccountTemporalConfigSchema,
  GetAccountTemporalConfigResponseSchema,
  UserAccountService,
} from '@husonym/sdk';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import Error from 'next/error';
import { ReactElement } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';

export default function Temporal(): ReactElement {
  const { account } = useAccount();
  const { data: systemAppConfigData, isLoading: isSystemAppConfigDataLoading } =
    useGetSystemAppConfig();
  const { data: tcData, isLoading: isTemporalConfigLoading } = useQuery(
    UserAccountService.method.getAccountTemporalConfig,
    { accountId: account?.id ?? '' },
    { enabled: !!account?.id }
  );
  const { mutateAsync } = useMutation(
    UserAccountService.method.setAccountTemporalConfig
  );
  const queryclient = useQueryClient();
  const t = useTranslations('settings.temporal');

  const form = useForm<TemporalFormValues>({
    resolver: yupResolver(TemporalFormValues),
    defaultValues: {
      namespace: 'default',
      syncJobName: 'sync-job',
      temporalUrl: 'localhost:7233',
    },
    values: {
      namespace: tcData?.config?.namespace ?? 'default',
      syncJobName: tcData?.config?.syncJobQueueName ?? 'sync-job',
      temporalUrl: tcData?.config?.url ?? 'localhost:7233',
    },
  });
  async function onSubmit(values: TemporalFormValues): Promise<void> {
    if (!account) {
      return;
    }
    try {
      const updatedResp = await mutateAsync({
        accountId: account.id,
        config: create(AccountTemporalConfigSchema, {
          namespace: values.namespace,
          syncJobQueueName: values.syncJobName,
          url: values.temporalUrl,
        }),
      });
      const key = createConnectQueryKey({
        schema: UserAccountService.method.getAccountTemporalConfig,
        input: { accountId: account.id },
        cardinality: undefined,
      });
      queryclient.setQueryData(
        key,
        create(GetAccountTemporalConfigResponseSchema, {
          config: updatedResp.config,
        })
      );
      toast.success(t('success'));
    } catch (err) {
      toast.error(t('error'), {
        description: getErrorMessage(err),
      });
    }
  }
  if (isSystemAppConfigDataLoading) {
    return <Skeleton />;
  }

  if (systemAppConfigData?.isHusonymCloud) {
    return <Error statusCode={404} />;
  }

  if (isTemporalConfigLoading) {
    return <Skeleton />;
  }

  return (
    <div className="flex flex-col gap-5">
      <SubPageHeader header={t('title')} description={t('description')} />
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="space-y-6 mt-10"
        >
          <div className="space-y-10 ">
            <FormField
              control={form.control}
              name="temporalUrl"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('urlLabel')}</FormLabel>
                  <FormDescription>{t('urlDescription')}</FormDescription>
                  <FormControl>
                    <Input placeholder={t('urlPlaceholder')} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="namespace"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('namespaceLabel')}</FormLabel>
                  <FormDescription>{t('namespaceDescription')}</FormDescription>
                  <FormControl>
                    <Input placeholder={t('namespacePlaceholder')} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="syncJobName"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('queueLabel')}</FormLabel>
                  <FormDescription>{t('queueDescription')}</FormDescription>
                  <FormControl>
                    <Input placeholder={t('queuePlaceholder')} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <div className="flex justify-end">
            <Button type="submit">{t('submit')}</Button>
          </div>
        </form>
      </Form>
    </div>
  );
}
