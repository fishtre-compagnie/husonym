import EditTransformerOptions from '@/app/(mgmt)/[account]/transformers/EditTransformerOptions';
import { FormDescription, FormLabel } from '@/components/ui/form';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import {
  getFilterdTransformersByType,
  getTransformerFromField,
  getTransformerSelectButtonText,
  isInvalidTransformer,
} from '@/util/util';
import {
  DynamoDBSourceOptionsFormValues,
  DynamoDBSourceUnmappedTransformConfigFormValues,
} from '@/yup-validations/jobs';
import { TransformerDataType } from '@husonym/sdk';
import { ExternalLinkIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import NextLink from 'next/link';
import { ReactElement } from 'react';
import TransformerSelect from '../SchemaTable/TransformerSelect';
import { TransformerHandler } from '../SchemaTable/transformer-handler';

interface Props {
  value: DynamoDBSourceOptionsFormValues;
  setValue(newVal: DynamoDBSourceOptionsFormValues): void;
  transformerHandler: TransformerHandler;
}
export default function DynamoDBSourceOptionsForm(props: Props): ReactElement {
  const { value, setValue, transformerHandler } = props;
  const t = useTranslations('mapping.form.dynamodbSource');
  return (
    <div className="flex flex-col gap-6 rounded-lg border p-4">
      <div className="flex flex-col gap-2">
        <div>
          <h2 className="text-md font-semibold tracking-tight">
            {t('unmappedTitle')}
          </h2>
          <p className="text-sm tracking-tight">{t('unmappedDescription')}</p>
        </div>
        <UnmappedTransformConfigForm
          value={value.unmappedTransformConfig}
          setValue={(newVal) =>
            setValue({ ...value, unmappedTransformConfig: newVal })
          }
          transformerHandler={transformerHandler}
        />
      </div>
      <div className="flex flex-col gap-2">
        <div>
          <h2 className="text-md font-semibold tracking-tight">
            {t('readConsistencyTitle')}
          </h2>
          <div className="inline-flex gap-1 flex-row">
            <p className="text-sm tracking-tight">
              {t('readConsistencyDefault')} <br />
              {t('readConsistencyStrong')}
              <br />
              {t('readMore')}{' '}
              <NextLink
                className="hover:underline inline-flex gap-1 flex-row items-center"
                href="https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.ReadConsistency.html"
                target="_blank"
              >
                {t('awsDocs')}
                <ExternalLinkIcon className="w-4 h-4 text-muted-foreground" />
              </NextLink>
            </p>
          </div>
        </div>
        <ConsistentReadForm
          value={value.enableConsistentRead}
          setValue={(checked) =>
            setValue({ ...value, enableConsistentRead: checked })
          }
        />
      </div>
    </div>
  );
}

interface UnmappedTransformConfigFormProps {
  value: DynamoDBSourceUnmappedTransformConfigFormValues;
  setValue(newVal: DynamoDBSourceUnmappedTransformConfigFormValues): void;
  transformerHandler: TransformerHandler;
}
function UnmappedTransformConfigForm(
  props: UnmappedTransformConfigFormProps
): ReactElement {
  const { value, setValue, transformerHandler } = props;
  const byteTransformer = getTransformerFromField(
    transformerHandler,
    value.byte
  );
  const boolTransformer = getTransformerFromField(
    transformerHandler,
    value.boolean
  );
  const numTransformer = getTransformerFromField(transformerHandler, value.n);
  const strTransformer = getTransformerFromField(transformerHandler, value.s);
  const t = useTranslations('mapping.form.dynamodbSource');
  return (
    <div className="flex flex-row gap-2 flex-wrap justify-between">
      <div className="flex flex-col gap-2">
        <div>
          <FormLabel>{t('byte')}</FormLabel>
          <FormDescription className="text-ellipsis">
            {t('byteDescription')}
          </FormDescription>
        </div>
        <div className="flex flex-row gap-2">
          <TransformerSelect
            getTransformers={() =>
              getFilterdTransformersByType(
                transformerHandler,
                TransformerDataType.ANY
              )
            }
            buttonText={getTransformerSelectButtonText(byteTransformer)}
            value={value.byte}
            onSelect={(newVal) => setValue({ ...value, byte: newVal })}
            disabled={false}
          />
          <EditTransformerOptions
            transformer={byteTransformer}
            value={value.byte}
            onSubmit={(newVal) => setValue({ ...value, byte: newVal })}
            disabled={isInvalidTransformer(byteTransformer)}
          />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <div>
          <FormLabel>{t('boolean')}</FormLabel>
          <FormDescription>{t('booleanDescription')}</FormDescription>
        </div>
        <div className="flex flex-row gap-2">
          <TransformerSelect
            getTransformers={() =>
              getFilterdTransformersByType(
                transformerHandler,
                TransformerDataType.BOOLEAN
              )
            }
            buttonText={getTransformerSelectButtonText(boolTransformer)}
            value={value.boolean}
            onSelect={(newVal) => setValue({ ...value, boolean: newVal })}
            disabled={false}
          />
          <EditTransformerOptions
            transformer={boolTransformer}
            value={value.boolean}
            onSubmit={(newVal) => setValue({ ...value, boolean: newVal })}
            disabled={isInvalidTransformer(boolTransformer)}
          />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <div>
          <FormLabel>{t('number')}</FormLabel>
          <FormDescription>{t('numberDescription')}</FormDescription>
        </div>
        <div className="flex flex-row gap-2">
          <TransformerSelect
            getTransformers={() =>
              getFilterdTransformersByType(
                transformerHandler,
                TransformerDataType.INT64
              )
            }
            buttonText={getTransformerSelectButtonText(numTransformer)}
            value={value.n}
            onSelect={(newVal) => setValue({ ...value, n: newVal })}
            disabled={false}
          />
          <EditTransformerOptions
            transformer={numTransformer}
            value={value.n}
            onSubmit={(newVal) => setValue({ ...value, n: newVal })}
            disabled={isInvalidTransformer(numTransformer)}
          />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <div>
          <FormLabel>{t('string')}</FormLabel>
          <FormDescription>{t('stringDescription')}</FormDescription>
        </div>
        <div className="flex flex-row gap-2">
          <TransformerSelect
            getTransformers={() =>
              getFilterdTransformersByType(
                transformerHandler,
                TransformerDataType.STRING
              )
            } // todo: filter this by type
            buttonText={getTransformerSelectButtonText(strTransformer)}
            value={value.s}
            onSelect={(newVal) => setValue({ ...value, s: newVal })}
            disabled={false}
          />
          <EditTransformerOptions
            transformer={strTransformer}
            value={value.s}
            onSubmit={(newVal) => setValue({ ...value, s: newVal })}
            disabled={isInvalidTransformer(strTransformer)}
          />
        </div>
      </div>
    </div>
  );
}

interface ConsistentReadFormProps {
  value: boolean;
  setValue(newValue: boolean): void;
}

function ConsistentReadForm(props: ConsistentReadFormProps): ReactElement {
  const { value, setValue } = props;
  const t = useTranslations('mapping.form.dynamodbSource');

  return (
    <ToggleGroup
      type="single"
      className="flex justify-start items-start flex-wrap"
      onValueChange={(newValue) => {
        // on value change is triggered with an empty string if the currently selected toggle is clicked
        if (newValue) {
          setValue(newValue === 'strong');
        }
      }}
      value={value ? 'strong' : 'eventual'}
    >
      <ToggleGroupItem className="border" value="eventual">
        {t('eventual')}
      </ToggleGroupItem>
      <ToggleGroupItem className="border" value="strong">
        {t('strong')}
      </ToggleGroupItem>
    </ToggleGroup>
  );
}
