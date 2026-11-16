'use client';
import { ExternalLinkIcon } from '@radix-ui/react-icons';
import { ReactElement, ReactNode } from 'react';

import { cn } from '@/libs/utils';
import { CreateTeamFormValues } from '@/yup-validations/account-switcher';
import Link from 'next/link';
import { UseFormReturn } from 'react-hook-form';
import { Button } from '../ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '../ui/form';
import { Input } from '../ui/input';
import { Switch } from '../ui/switch';
import { useTranslations } from 'next-intl';

interface Props {
  open: boolean;
  onOpenChange(val: boolean): void;
  trigger?: ReactNode;

  onSubmit(values: CreateTeamFormValues): Promise<void>;
  onCancel(): void;
  showSubscriptionInfo: boolean;
  showConvertPersonalToTeamOption: boolean;
  form: UseFormReturn<CreateTeamFormValues>;
}

export function CreateNewTeamDialog(props: Props): ReactElement {
  const {
    open,
    onOpenChange,
    trigger,
    onCancel,
    onSubmit,
    showSubscriptionInfo,
    showConvertPersonalToTeamOption,
    form,
  } = props;
  const t = useTranslations('account');
  const tCommon = useTranslations('common');

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger}
      <DialogContent className="flex flex-col gap-3">
        <DialogHeader>
          <DialogTitle>{t('createTeam')}</DialogTitle>
          <DialogDescription>
            {t('createTeamDialog.description')}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2 py-2">
          <Form {...form}>
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('createTeamDialog.name')}</FormLabel>
                  <FormControl>
                    <Input
                      autoCapitalize="off" // we don't allow capitals in team names
                      data-1p-ignore // tells 1password extension to not autofill this field
                      placeholder="acme"
                      {...field}
                    />
                  </FormControl>
                  <FormMessage className="mt-0" />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="convertPersonalToTeam"
              render={({ field }) => (
                <FormItem
                  className={cn(
                    showConvertPersonalToTeamOption ? undefined : 'hidden'
                  )}
                >
                  <FormLabel>{t('createTeamDialog.convert')}</FormLabel>
                  <FormDescription>
                    {t('createTeamDialog.convertDescription')}
                  </FormDescription>
                  <FormControl className="m-0">
                    <Switch
                      ref={field.ref}
                      onBlur={field.onBlur}
                      onCheckedChange={(newval) => field.onChange(newval)}
                      checked={field.value}
                      disabled={field.disabled}
                      name={field.name}
                    />
                  </FormControl>
                </FormItem>
              )}
            />
          </Form>
          {showSubscriptionInfo && <ShowSubscriptionInfo />}
        </div>
        <DialogFooter>
          <div className="flex flex-row justify-between w-full">
            <Button variant="outline" onClick={() => onCancel()}>
              {tCommon('cancel')}
            </Button>
            <Button
              type="submit"
              onClick={(e) =>
                form.handleSubmit((values) => onSubmit(values))(e)
              }
              disabled={!form.formState.isValid}
            >
              {tCommon('continue')}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface ShowSubscriptionInfoProps {}
function ShowSubscriptionInfo(props: ShowSubscriptionInfoProps): ReactElement {
  const {} = props;
  const t = useTranslations('account.createTeamDialog');

  return (
    <div>
      <div className="flex flex-row gap-2">
        <p className="text-sm tracking-tight">{t('subscription')}</p>
        <Link
          href="https://husonym.com/pricing"
          target="_blank"
          className="hover:underline inline-flex gap-1 flex-row items-center text-sm tracking-tight"
        >
          {t('learnMore')}
          <ExternalLinkIcon className="w-3 h-3" />
        </Link>
      </div>
    </div>
  );
}
