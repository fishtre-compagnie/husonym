'use client';

import ButtonText from '@/components/ButtonText';
import ConfirmationDialog from '@/components/ConfirmationDialog';
import SwitchCard from '@/components/switches/SwitchCard';
import { Button } from '@/components/ui/button';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form';
import { DefaultTransformerFormValues } from '@/yup-validations/jobs';
import { yupResolver } from '@/util/yup-form-resolver';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { useForm, UseFormReturn } from 'react-hook-form';
interface Props {
  isDisabled: boolean;
  onClick(override: boolean): void;
}

export default function ApplyDefaultTransformersButton(
  props: Props
): ReactElement {
  const { isDisabled, onClick } = props;
  const t = useTranslations('mapping.schemaTable.applyDefault');
  const form = useForm<DefaultTransformerFormValues>({
    resolver: yupResolver(DefaultTransformerFormValues),
    defaultValues: {
      overrideTransformers: false,
    },
  });
  return (
    <ConfirmationDialog
      trigger={
        <Button variant="outline" type="button" disabled={isDisabled}>
          <ButtonText text={t('button')} />
        </Button>
      }
      headerText={t('header')}
      description={t('description')}
      body={<FormBody form={form} />}
      containerClassName="max-w-xl"
      buttonText={t('confirm')}
      onConfirm={() => {
        const override = form.getValues('overrideTransformers');
        onClick(override);
      }}
    />
  );
}

interface FormBodyProps {
  form: UseFormReturn<DefaultTransformerFormValues>;
}
function FormBody(props: FormBodyProps): ReactElement {
  const { form } = props;
  const t = useTranslations('mapping.schemaTable.applyDefault');
  return (
    <div>
      <Form {...form}>
        <form className="space-y-8">
          <FormField
            control={form.control}
            name="overrideTransformers"
            render={({ field }) => (
              <FormItem>
                <FormControl>
                  <SwitchCard
                    isChecked={field.value}
                    onCheckedChange={field.onChange}
                    title={t('overrideTitle')}
                    description={t('overrideDescription')}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </div>
  );
}
