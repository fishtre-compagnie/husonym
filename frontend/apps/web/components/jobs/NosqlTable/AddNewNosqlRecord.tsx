import EditTransformerOptions from '@/app/(mgmt)/[account]/transformers/EditTransformerOptions';
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { cn } from '@/libs/utils';
import {
  getTransformerFromField,
  getTransformerSelectButtonText,
  isInvalidTransformer,
} from '@/util/util';
import {
  convertJobMappingTransformerToForm,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { create, MessageInitShape } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  ConnectError,
  JobMappingTransformerSchema,
  PassthroughSchema,
  SystemTransformerSchema,
  TransformerConfigSchema,
  TransformersService,
  ValidateUserJavascriptCodeRequestSchema,
  ValidateUserJavascriptCodeResponse,
} from '@husonym/sdk';
import { UseMutateAsyncFunction } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { ReactElement, useMemo } from 'react';
import { useForm } from 'react-hook-form';
import * as Yup from 'yup';
import { TransformerHandler } from '../SchemaTable/transformer-handler';
import TransformerSelect from '../SchemaTable/TransformerSelect';

interface Props {
  collections: string[];
  onSubmit(values: AddNewNosqlRecordFormValues): void;
  transformerHandler: TransformerHandler;
  isDuplicateKey: (
    value: string,
    schema: string,
    table: string,
    currValue?: string
  ) => boolean;
}

function getAddNewNosqlRecordFormValues(
  t: ReturnType<typeof useTranslations<'mapping.nosql.addRecord'>>
) {
  return Yup.object({
    collection: Yup.string().required(t('collectionRequired')),
    key: Yup.string()
      .required(t('keyRequired'))
      .test({
        name: 'uniqueMapping',
        message: t('keyExistsInSelected'),
        test: function (value, context) {
          const { collection } = this.parent;

          if (!collection || !value) {
            return true;
          }

          const lastDotIndex = collection.lastIndexOf('.');
          const schema = collection.substring(0, lastDotIndex);
          const table = collection.substring(lastDotIndex + 1);

          return (
            !context?.options?.context?.isDuplicateKey(value, schema, table) ||
            this.createError({
              message: t('keyExists'),
            })
          );
        },
      }),
    transformer: JobMappingTransformerForm,
  });
}

export type AddNewNosqlRecordFormValues = Yup.InferType<
  ReturnType<typeof getAddNewNosqlRecordFormValues>
>;

interface AddNewNosqlRecordFormContext {
  accountId: string;
  isUserJavascriptCodeValid: UseMutateAsyncFunction<
    ValidateUserJavascriptCodeResponse,
    ConnectError,
    MessageInitShape<typeof ValidateUserJavascriptCodeRequestSchema>,
    unknown
  >;
  isDuplicateKey: (value: string, schema: string, table: string) => boolean;
}

export default function AddNewNosqlRecord(props: Props): ReactElement {
  const { collections, onSubmit, transformerHandler, isDuplicateKey } = props;
  const t = useTranslations('mapping.nosql.addRecord');
  const formSchema = useMemo(() => getAddNewNosqlRecordFormValues(t), [t]);

  const { account } = useAccount();
  const { mutateAsync: validateUserJsCodeAsync } = useMutation(
    TransformersService.method.validateUserJavascriptCode
  );
  const form = useForm<
    AddNewNosqlRecordFormValues,
    AddNewNosqlRecordFormContext
  >({
    resolver: yupResolver(formSchema),
    mode: 'onChange',
    defaultValues: {
      collection: '',
      key: '',
      transformer: convertJobMappingTransformerToForm(
        create(JobMappingTransformerSchema, {
          config: create(TransformerConfigSchema, {
            config: {
              case: 'passthroughConfig',
              value: create(PassthroughSchema),
            },
          }),
        })
      ),
    },
    context: {
      accountId: account?.id ?? '',
      isUserJavascriptCodeValid: validateUserJsCodeAsync,
      isDuplicateKey: isDuplicateKey,
    },
  });

  return (
    <div className="flex flex-col w-full">
      <Form {...form}>
        <FormField
          control={form.control}
          name="collection"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('collection')}</FormLabel>
              <FormDescription>{t('collectionDescription')}</FormDescription>
              <FormControl>
                <Select
                  onValueChange={(value) => {
                    field.onChange(value);
                    form.clearErrors('key');
                    const currentKey = form.getValues('key');
                    if (currentKey) {
                      const lastDotIndex = value.lastIndexOf('.');
                      const schema = value.substring(0, lastDotIndex);
                      const table = value.substring(lastDotIndex + 1);
                      if (isDuplicateKey(currentKey, schema, table)) {
                        form.setError('key', {
                          type: 'manual',
                          message: t('keyExistsInSelected'),
                        });
                      }
                    }
                  }}
                  value={field.value}
                >
                  <SelectTrigger
                    className={cn(
                      field.value ? undefined : 'text-muted-foreground'
                    )}
                  >
                    <SelectValue placeholder={t('collectionPlaceholder')} />
                  </SelectTrigger>
                  <SelectContent>
                    {collections.map((collection) => (
                      <SelectItem value={collection} key={collection}>
                        {collection}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="key"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('documentKey')}</FormLabel>
              <FormDescription>{t('documentKeyDescription')}</FormDescription>
              <FormControl>
                <Input
                  {...field}
                  placeholder="users.address.city"
                  disabled={!form.getValues('collection')}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="transformer"
          render={({ field }) => {
            const fv = field.value;
            const transformer = getTransformerFromField(transformerHandler, fv);
            return (
              <FormItem>
                <FormLabel>{t('transformer')}</FormLabel>
                <FormDescription>{t('transformerDescription')}</FormDescription>
                <FormControl>
                  <div className="flex flex-row gap-2">
                    <div>
                      <TransformerSelect
                        getTransformers={() =>
                          transformerHandler.getTransformers()
                        }
                        buttonText={getTransformerSelectButtonText(transformer)}
                        value={fv}
                        onSelect={field.onChange}
                        side={'left'}
                        disabled={false}
                        buttonClassName="w-[175px]"
                      />
                    </div>
                    <EditTransformerOptions
                      transformer={
                        transformer ?? create(SystemTransformerSchema)
                      }
                      value={fv}
                      onSubmit={(newvalue) => {
                        field.onChange(newvalue);
                      }}
                      disabled={isInvalidTransformer(transformer)}
                    />
                  </div>
                </FormControl>
                <FormMessage />
              </FormItem>
            );
          }}
        />
        <div className="flex justify-end">
          <Button
            type="button"
            disabled={!form.formState.isValid}
            onClick={(e) =>
              form.handleSubmit((values) => {
                onSubmit(values);
                form.resetField('key');
                form.resetField('transformer');
              })(e)
            }
          >
            {t('add')}
          </Button>
        </div>
      </Form>
    </div>
  );
}
