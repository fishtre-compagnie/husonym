'use client';
import { ApiKeyValueSessionStore } from '@/app/(mgmt)/[account]/new/api-key/NewApiKeyForm';
import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import SubPageHeader from '@/components/headers/SubPageHeader';
import RequiredLabel from '@/components/labels/RequiredLabel';
import { useAccount } from '@/components/providers/account-provider';
import SkeletonForm from '@/components/skeleton/SkeletonForm';
import { PageProps } from '@/components/types';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Calendar } from '@/components/ui/calendar';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { cn } from '@/libs/utils';
import { getErrorMessage } from '@/util/util';
import { RegenerateApiKeyForm } from '@/yup-validations/apikey';
import { create } from '@bufbuild/protobuf';
import { timestampFromMs } from '@bufbuild/protobuf/wkt';
import {
  createConnectQueryKey,
  useMutation,
  useQuery,
} from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  ApiKeyService,
  GetAccountApiKeyResponseSchema,
  RegenerateAccountApiKeyRequestSchema,
} from '@husonym/sdk';
import { CalendarIcon } from '@radix-ui/react-icons';
import { useQueryClient } from '@tanstack/react-query';
import { addDays, endOfDay, format, startOfDay } from 'date-fns';
import { useDateFnsLocale } from '@/i18n/date-fns';
import { useTranslations } from 'next-intl';
import Error from 'next/error';
import { useRouter } from 'next/navigation';
import { ReactElement, use } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';

export default function RegenerateAccountApiKey(
  props: PageProps
): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  const router = useRouter();
  const { account } = useAccount();
  const { allowed } = useLicenseFeature('api_keys');
  const t = useTranslations('settings.apiKeys.regenerate');
  const tForm = useTranslations('settings.apiKeys.form');
  const tDetail = useTranslations('settings.apiKeys.detail');
  const dateLocale = useDateFnsLocale();
  const { data, isLoading } = useQuery(
    ApiKeyService.method.getAccountApiKey,
    { id },
    { enabled: !!id }
  );
  const { mutateAsync } = useMutation(
    ApiKeyService.method.regenerateAccountApiKey
  );
  const queryclient = useQueryClient();

  const form = useForm({
    resolver: yupResolver(RegenerateApiKeyForm),
    defaultValues: {
      expiresAtSelect: '7',
      expiresAt: startOfDay(addDays(new Date(), 7)),
    },
  });

  async function onSubmit(values: RegenerateApiKeyForm): Promise<void> {
    if (!id) {
      return;
    }
    try {
      const updatedApiKey = await mutateAsync(
        create(RegenerateAccountApiKeyRequestSchema, {
          id,
          expiresAt: timestampFromMs(values.expiresAt.getTime()),
        })
      );
      if (updatedApiKey.apiKey?.keyValue && !!window?.sessionStorage) {
        const storeVal: ApiKeyValueSessionStore = {
          keyValue: updatedApiKey.apiKey.keyValue,
        };
        window.sessionStorage.setItem(id, JSON.stringify(storeVal));
      }
      const key = createConnectQueryKey({
        schema: ApiKeyService.method.getAccountApiKey,
        input: { id },
        cardinality: undefined,
      });
      queryclient.setQueryData(
        key,
        create(GetAccountApiKeyResponseSchema, {
          apiKey: updatedApiKey.apiKey,
        })
      );
      router.push(`/${account?.name}/settings/api-keys/${id}`);
      toast.success(t('success'));
    } catch (err) {
      console.error(err);
      toast.error(t('error'), {
        description: getErrorMessage(err),
      });
    }
  }

  if (!id) {
    return <Error statusCode={404} />;
  }
  if (isLoading) {
    return (
      <div>
        <SkeletonForm />
      </div>
    );
  }
  if (!data?.apiKey) {
    return (
      <div className="mt-10">
        <Alert variant="destructive">
          <AlertTitle>{tDetail('loadError')}</AlertTitle>
        </Alert>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-5">
      <SubPageHeader
        header={t('title', { name: data.apiKey.name })}
        description={t('description')}
      />
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className="flex flex-col gap-8"
        >
          <LicenseFeatureNotice feature="api_keys" />
          <FormField
            control={form.control}
            name="expiresAtSelect"
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {tForm('expiration')} <RequiredLabel />
                </FormLabel>
                <Select
                  onValueChange={(value) => {
                    field.onChange(value);
                    if (value !== 'custom') {
                      form.setValue(
                        'expiresAt',
                        addDays(startOfDay(new Date()), parseInt(value, 10))
                      );
                    }
                  }}
                  value={field.value}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue
                        placeholder={tForm('expirationPlaceholder')}
                      />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    <SelectItem className="cursor-pointer" value="7">
                      {tForm('days', { count: 7 })}
                    </SelectItem>
                    <SelectItem className="cursor-pointer" value="30">
                      {tForm('days', { count: 30 })}
                    </SelectItem>
                    <SelectItem className="cursor-pointer" value="60">
                      {tForm('days', { count: 60 })}
                    </SelectItem>
                    <SelectItem className="cursor-pointer" value="90">
                      {tForm('days', { count: 90 })}
                    </SelectItem>
                    <SelectItem className="cursor-pointer" value="custom">
                      {tForm('custom')}
                    </SelectItem>
                  </SelectContent>
                </Select>
                <FormDescription>
                  <p>
                    {tForm('expiresOn', {
                      date: format(form.getValues().expiresAt, 'PPP', {
                        locale: dateLocale,
                      }),
                    })}
                  </p>
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="expiresAt"
            render={({ field }) => (
              <FormItem
                className={cn(
                  'flex flex-col',
                  form.watch('expiresAtSelect') !== 'custom' && 'hidden'
                )}
              >
                <Popover>
                  <PopoverTrigger asChild>
                    <FormControl>
                      <Button
                        disabled={form.watch('expiresAtSelect') !== 'custom'}
                        variant={'outline'}
                        className={cn(
                          'w-[240px] pl-3 text-left font-normal',
                          !field.value && 'text-muted-foreground'
                        )}
                      >
                        {field.value ? (
                          format(field.value, 'PPP', { locale: dateLocale })
                        ) : (
                          <span>{tForm('pickDate')}</span>
                        )}
                        <CalendarIcon className="ml-auto h-4 w-4 opacity-50" />
                      </Button>
                    </FormControl>
                  </PopoverTrigger>
                  <PopoverContent className="w-auto p-0" align="start">
                    <Calendar
                      mode="single"
                      selected={field.value}
                      onSelect={(val) => {
                        field.onChange(startOfDay(val ?? new Date()));
                      }}
                      disabled={(date) =>
                        date < endOfDay(new Date()) ||
                        // must be days instead of years to account for leap year
                        // backend constraints to within 365 days of the current time
                        date > addDays(startOfDay(new Date()), 365)
                      }
                    />
                  </PopoverContent>
                </Popover>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className="flex flex-row justify-end">
            <Button type="submit" disabled={!allowed}>
              {tForm('submit')}
            </Button>
          </div>
        </form>
      </Form>
    </div>
  );
}
