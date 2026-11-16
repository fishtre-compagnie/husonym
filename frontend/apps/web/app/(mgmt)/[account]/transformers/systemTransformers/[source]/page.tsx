'use client';
import TransformerForm from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/TransformerForm';
import ButtonText from '@/components/ButtonText';
import { PageBody, PageHeader } from '@/components/ds';
import LearnMoreLink from '@/components/labels/LearnMoreLink';
import { useAccount } from '@/components/providers/account-provider';
import { PageProps } from '@/components/types';
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
import {
  getTransformerDataTypesString,
  getTransformerSourceString,
} from '@/util/util';
import {
  convertTransformerConfigSchemaToTransformerConfig,
  convertTransformerConfigToForm,
} from '@/yup-validations/jobs';
import { useQuery } from '@connectrpc/connect-query';
import { TransformerSource, TransformersService } from '@husonym/sdk';
import Error from 'next/error';
import NextLink from 'next/link';
import { ReactElement, use } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslations } from 'next-intl';
import { constructDocsLink } from '../../EditTransformerOptions';

function getTransformerSource(sourceStr: string): TransformerSource {
  const sourceNum = parseInt(sourceStr, 10);
  if (isNaN(sourceNum) || !TransformerSource[sourceNum]) {
    return TransformerSource.UNSPECIFIED;
  }
  return sourceNum as TransformerSource;
}

export default function ViewSystemTransformers(props: PageProps): ReactElement {
  const params = use(props.params);
  const t = useTranslations('connections.transformers.system');
  const sourceParam = getTransformerSource(params?.source ?? '');
  const { data: systemTransformerData, isLoading } = useQuery(
    TransformersService.method.getSystemTransformerBySource,
    { source: sourceParam },
    { enabled: !!sourceParam }
  );
  const { account } = useAccount();
  const systemTransformer = systemTransformerData?.transformer;

  const form = useForm({
    values: {
      name: systemTransformer?.name ?? '',
      description: systemTransformer?.description ?? '',
      type: getTransformerDataTypesString(systemTransformer?.dataTypes ?? []),
      source: getTransformerSourceString(
        systemTransformer?.source ?? TransformerSource.UNSPECIFIED
      ),
      config: convertTransformerConfigToForm(systemTransformer?.config),
    },
  });

  if (isLoading) {
    return <Skeleton />;
  }
  if (!systemTransformer) {
    return <Error statusCode={404} />;
  }

  const cfg = form.watch('config');

  return (
    <PageBody className="max-w-[1320px]">
      <PageHeader
        title={systemTransformer?.name ?? t('fallbackTitle')}
        actions={
          <CloneTransformerButton source={systemTransformer?.source ?? ''} />
        }
      />
      <Form {...form}>
        <form>
          <FormField
            control={form.control}
            name="name"
            disabled={true}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('nameLabel')}</FormLabel>
                <FormDescription>
                  {t('nameDescription')}{' '}
                  <LearnMoreLink
                    href={constructDocsLink(
                      getTransformerSource(String(sourceParam))
                    )}
                  />
                </FormDescription>
                <FormControl>
                  <Input placeholder={t('namePlaceholder')} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="description"
            disabled={true}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('descriptionLabel')}</FormLabel>
                <FormDescription>{t('descriptionDescription')}</FormDescription>
                <FormControl>
                  <Input placeholder={t('namePlaceholder')} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            disabled={true}
            name="type"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('typeLabel')}</FormLabel>
                <FormDescription>{t('typeDescription')}</FormDescription>
                <FormControl>
                  <Input placeholder={t('typePlaceholder')} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            disabled={true}
            name="source"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('sourceLabel')}</FormLabel>
                <FormDescription>{t('sourceDescription')}</FormDescription>
                <FormControl>
                  <Input placeholder={t('sourcePlaceholder')} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <div>
            <TransformerForm
              value={convertTransformerConfigSchemaToTransformerConfig(cfg)}
              setValue={() => undefined}
              disabled={true}
            />
          </div>
          <div className="flex flex-row justify-start pt-10">
            <NextLink href={`/${account?.name}/transformers?tab=system`}>
              <Button type="button">{t('back')}</Button>
            </NextLink>
          </div>
        </form>
      </Form>
    </PageBody>
  );
}

interface CloneTransformerProps {
  source: TransformerSource;
}

function CloneTransformerButton(props: CloneTransformerProps): ReactElement {
  const { source } = props;
  const t = useTranslations('connections.transformers.system');
  const { account } = useAccount();
  return (
    <NextLink href={`/${account?.name}/new/transformer?transformer=${source}`}>
      <Button>
        <ButtonText text={t('clone')} />
      </Button>
    </NextLink>
  );
}
