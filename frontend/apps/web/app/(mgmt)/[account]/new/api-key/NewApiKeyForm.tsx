'use client';
import LicenseFeatureNotice from '@/components/LicenseFeatureNotice';
import RequiredLabel from '@/components/labels/RequiredLabel';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { endOfDay, format, startOfDay } from 'date-fns';

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
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent } from '@/components/ui/popover';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { cn } from '@/libs/utils';
import { getErrorMessage } from '@/util/util';
import { ApiKeyFormValues } from '@/yup-validations/apikey';
import { timestampFromMs } from '@bufbuild/protobuf/wkt';
import { useMutation } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import { ApiKeyService } from '@husonym/sdk';
import { CalendarIcon } from '@radix-ui/react-icons';
import { PopoverTrigger } from '@radix-ui/react-popover';
import { addDays } from 'date-fns';
import { useRouter } from 'next/navigation';
import { useDateFnsLocale } from '@/i18n/date-fns';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { PermissionsField } from './permissions';

export interface ApiKeyValueSessionStore {
  keyValue: string;
}

export default function NewApiKeyForm(): ReactElement {
  const { account } = useAccount();
  const { allowed } = useLicenseFeature('api_keys');
  const router = useRouter();
  const t = useTranslations('settings.apiKeys.new');
  const tForm = useTranslations('settings.apiKeys.form');
  const dateLocale = useDateFnsLocale();
  const form = useForm({
    mode: 'onChange',
    resolver: yupResolver(ApiKeyFormValues),
    defaultValues: {
      name: '',
      expiresAtSelect: '7',
      expiresAt: startOfDay(addDays(new Date(), 7)),
      permissions: [],
    },
  });
  const { mutateAsync } = useMutation(ApiKeyService.method.createAccountApiKey);

  async function onSubmit(values: ApiKeyFormValues): Promise<void> {
    if (!account) {
      return;
    }
    try {
      const apiKey = await mutateAsync({
        accountId: account.id,
        expiresAt: timestampFromMs(values.expiresAt.getTime()),
        name: values.name,
        permissions: values.permissions,
      });
      if (apiKey.apiKey?.id) {
        if (apiKey.apiKey.keyValue && !!window?.sessionStorage) {
          const storeVal: ApiKeyValueSessionStore = {
            keyValue: apiKey.apiKey.keyValue,
          };
          window.sessionStorage.setItem(
            apiKey.apiKey.id,
            JSON.stringify(storeVal)
          );
        }
        router.push(`/${account?.name}/settings/api-keys/${apiKey.apiKey.id}`);
      } else {
        router.push(`/${account?.name}/settings/api-keys`);
      }
      toast.success(t('success'));
    } catch (err) {
      console.error(err);
      toast.error(t('error'), {
        description: getErrorMessage(err),
      });
    }
  }

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(onSubmit)}
        className="flex flex-col gap-4"
      >
        <LicenseFeatureNotice feature="api_keys" />
        <FormField
          control={form.control}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {tForm('name')} <RequiredLabel />
              </FormLabel>
              <FormControl>
                <Input placeholder={tForm('namePlaceholder')} {...field} />
              </FormControl>
              <FormDescription>{tForm('nameDescription')}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

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
                    <SelectValue placeholder={tForm('expirationPlaceholder')} />
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
                {tForm('expiresOn', {
                  date: format(form.getValues().expiresAt, 'PPP', {
                    locale: dateLocale,
                  }),
                })}
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
        <FormField
          control={form.control}
          name="permissions"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {tForm('permissions')} <RequiredLabel />
              </FormLabel>
              <FormDescription>
                {tForm('permissionsDescription')}
              </FormDescription>
              <FormControl>
                <PermissionsField
                  value={field.value}
                  onChange={field.onChange}
                />
              </FormControl>
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
  );
}
