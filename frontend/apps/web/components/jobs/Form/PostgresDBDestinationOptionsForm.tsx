import FormErrorMessage from '@/components/FormErrorMessage';
import FormHeader from '@/components/FormHeader';
import NumberedInput from '@/components/NumberedInput';
import SwitchCard from '@/components/switches/SwitchCard';
import { Input } from '@/components/ui/input';
import { PostgresDbDestinationOptionsFormValues } from '@/yup-validations/jobs';
import { useTranslations } from 'next-intl';
import { ReactElement, ReactNode } from 'react';
import { FieldErrors } from 'react-hook-form';

interface Props {
  value: PostgresDbDestinationOptionsFormValues;
  setValue(newVal: PostgresDbDestinationOptionsFormValues): void;
  errors?: FieldErrors<PostgresDbDestinationOptionsFormValues>;
  hideInitTableSchema?: boolean;
}

export default function PostgresDBDestinationOptionsForm(
  props: Props
): ReactElement {
  const { value, setValue, errors, hideInitTableSchema } = props;
  const t = useTranslations('mapping.form.destinationOptions');
  return (
    <div className="flex flex-col gap-2">
      <FormItemContainer>
        <SwitchCard
          isChecked={value.truncateBeforeInsert ?? false}
          onCheckedChange={(newVal) => {
            setValue({
              ...value,
              truncateBeforeInsert: newVal,
              truncateCascade: newVal
                ? (value?.truncateCascade ?? false)
                : false,
            });
          }}
          title={t('truncateBeforeInsert')}
          description={t('truncateBeforeInsertDescription')}
        />
        <FormErrorMessage message={errors?.truncateBeforeInsert?.message} />
      </FormItemContainer>
      <FormItemContainer>
        <SwitchCard
          isChecked={value.truncateCascade ?? false}
          onCheckedChange={(newVal) => {
            setValue({
              ...value,
              truncateBeforeInsert:
                newVal && !value.truncateBeforeInsert
                  ? true
                  : (value.truncateBeforeInsert ?? false),
              truncateCascade: newVal,
            });
          }}
          title={t('truncateCascade')}
          description={t('truncateCascadeDescription')}
        />
        <FormErrorMessage message={errors?.truncateCascade?.message} />
      </FormItemContainer>
      {!hideInitTableSchema && (
        <FormItemContainer>
          <SwitchCard
            isChecked={value.initTableSchema ?? false}
            onCheckedChange={(newVal) => {
              setValue({
                ...value,
                initTableSchema: newVal ?? false,
              });
            }}
            title={t('initTableSchema')}
            description={t('initTableSchemaDescription')}
          />
          <FormErrorMessage message={errors?.initTableSchema?.message} />
        </FormItemContainer>
      )}
      <FormItemContainer>
        <SwitchCard
          isChecked={value.conflictStrategy?.onConflictDoNothing ?? false}
          onCheckedChange={(newVal) => {
            setValue({
              ...value,
              conflictStrategy: {
                onConflictDoNothing: newVal,
                onConflictDoUpdate: false,
              },
            });
          }}
          title={t('onConflictDoNothing')}
          description={t('onConflictDoNothingDescription')}
        />
        <FormErrorMessage
          message={errors?.conflictStrategy?.onConflictDoNothing?.message}
        />
      </FormItemContainer>
      <FormItemContainer>
        <SwitchCard
          isChecked={value.conflictStrategy?.onConflictDoUpdate ?? false}
          onCheckedChange={(newVal) => {
            setValue({
              ...value,
              conflictStrategy: {
                onConflictDoNothing: false,
                onConflictDoUpdate: newVal,
              },
            });
          }}
          title={t('onConflictDoUpdate')}
          description={t('onConflictDoUpdateDescription')}
        />
        <FormErrorMessage
          message={errors?.conflictStrategy?.onConflictDoUpdate?.message}
        />
      </FormItemContainer>
      <FormItemContainer>
        <SwitchCard
          isChecked={value.skipForeignKeyViolations ?? false}
          onCheckedChange={(newVal) => {
            setValue({
              ...value,
              skipForeignKeyViolations: newVal,
            });
          }}
          title={t('skipForeignKeyViolations')}
          description={t('skipForeignKeyViolationsDescription')}
        />
        <FormErrorMessage message={errors?.skipForeignKeyViolations?.message} />
      </FormItemContainer>

      <FormItemBorder>
        <FormItemContainer>
          <FormHeader
            title={t('maxInFlight')}
            description={t('maxInFlightDescription')}
            isErrored={!!errors?.maxInFlight}
          />
          <div>
            <NumberedInput
              value={value.maxInFlight ?? -1}
              onChange={(val) => setValue({ ...value, maxInFlight: val })}
            />
            <FormErrorMessage message={errors?.maxInFlight?.message} />
          </div>
        </FormItemContainer>
      </FormItemBorder>

      <FormItemBorder>
        <FormItemContainer>
          <FormHeader
            title={t('batchCount')}
            description={t('batchCountDatabaseDescription')}
            isErrored={!!errors?.batch?.count}
          />
          <FormInputContainer>
            <NumberedInput
              value={value.batch?.count ?? -1}
              onChange={(val) =>
                setValue({ ...value, batch: { ...value.batch, count: val } })
              }
            />
            <FormErrorMessage message={errors?.batch?.count?.message} />
          </FormInputContainer>
        </FormItemContainer>
      </FormItemBorder>
      <FormItemBorder>
        <FormItemContainer>
          <FormHeader
            title={t('batchPeriod')}
            description={t('batchPeriodDescription')}
            isErrored={!!errors?.batch?.period}
          />
          <FormInputContainer>
            <Input
              value={value.batch?.period ?? ''}
              onChange={(e) =>
                setValue({
                  ...value,
                  batch: { ...value.batch, period: e.target.value },
                })
              }
            />
            <FormErrorMessage message={errors?.batch?.period?.message} />
          </FormInputContainer>
        </FormItemContainer>
      </FormItemBorder>
    </div>
  );
}

interface FormItemContainerProps {
  children: ReactNode;
}
function FormItemContainer(props: FormItemContainerProps): ReactElement {
  const { children } = props;
  return <div className="flex flex-col gap-2">{children}</div>;
}

interface FormInputContainerProps {
  children: ReactNode;
}
function FormInputContainer(props: FormInputContainerProps): ReactElement {
  const { children } = props;
  return <div className="flex flex-col gap-1">{children}</div>;
}

interface FormItemBorderProps {
  children: ReactNode;
}
function FormItemBorder(props: FormItemBorderProps): ReactElement {
  const { children } = props;
  return (
    <div className="rounded-xl border border-line-soft bg-surface-2/40 p-3">
      {children}
    </div>
  );
}
