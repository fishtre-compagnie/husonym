import TransformerForm from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/TransformerForm';
import ColumnPreview from '@/components/jobs/JobMappingTable/ColumnPreview';
import { TransformerResult } from '@/components/jobs/SchemaTable/transformer-handler';
import TransformerSelect from '@/components/jobs/SchemaTable/TransformerSelect';
import { useAccount } from '@/components/providers/account-provider';
import { Transformer } from '@/shared/transformers';
import { Form } from '@/components/ui/form';
import { Label } from '@/components/ui/label';
import { useDebouncedValue } from '@/libs/hooks/useDebouncedValue';
import { getTransformerSelectButtonText } from '@/util/util';
import { yupResolver } from '@/util/yup-form-resolver';
import {
  EditJobMappingTransformerConfigFormContext,
  EditJobMappingTransformerConfigFormValues,
} from '@/yup-validations/transformer-validations';
import {
  convertJobMappingTransformerFormToJobMappingTransformer,
  convertJobMappingTransformerToForm,
  convertTransformerConfigToForm,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { create, toJsonString } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import {
  JobMappingTransformerSchema,
  TransformerConfig,
  TransformerConfigSchema,
  TransformersService,
} from '@husonym/sdk';
import { ReactElement, ReactNode, useEffect } from 'react';
import { useForm } from 'react-hook-form';

export interface PreviewTarget {
  connectionId: string;
  schema: string;
  table: string;
  column: string;
}

interface Props {
  value: JobMappingTransformerForm;
  onChange(value: JobMappingTransformerForm): void;
  // Filtrés par type, et sur la page Source par contraintes de colonne.
  getTransformers(): TransformerResult;
  selected: Transformer;
  disabled?: boolean;
  // Absent pour un job generate : il n'y a aucune donnée à lire.
  preview?: PreviewTarget;
  onValidChange?(valid: boolean): void;
  placeholder?: string;
  origin?: ReactNode;
}

export function toConfig(
  transformer: JobMappingTransformerForm
): TransformerConfig | undefined {
  if (!transformer.config.case) {
    return undefined;
  }
  return convertJobMappingTransformerFormToJobMappingTransformer(transformer)
    .config;
}

// Deux configs identiques partagent la clé, même si les objets diffèrent.
export function configKey(transformer: JobMappingTransformerForm): string {
  const config = toConfig(transformer);
  return config ? toJsonString(TransformerConfigSchema, config) : '';
}

// Le transformer, ses options et ce qu'ils font des valeurs de la colonne.
export default function ColumnDecisionFields(props: Props): ReactElement {
  const {
    value,
    onChange,
    getTransformers,
    selected,
    disabled = false,
    preview,
    onValidChange,
    placeholder = 'Choose a transformer',
    origin,
  } = props;

  const { account } = useAccount();
  const { mutateAsync: isJavascriptCodeValid } = useMutation(
    TransformersService.method.validateUserJavascriptCode
  );

  const config = toConfig(value);
  // Le parent porte la valeur ; ce formulaire ne sert qu'à valider les options.
  const form = useForm<
    EditJobMappingTransformerConfigFormValues,
    EditJobMappingTransformerConfigFormContext
  >({
    mode: 'onChange',
    resolver: yupResolver(EditJobMappingTransformerConfigFormValues),
    defaultValues: {
      config: convertTransformerConfigToForm(
        config ?? create(TransformerConfigSchema)
      ),
    },
    context: {
      accountId: account?.id ?? '',
      isUserJavascriptCodeValid: isJavascriptCodeValid,
    },
  });

  const { isValid } = form.formState;
  useEffect(() => {
    onValidChange?.(isValid);
  }, [isValid, onValidChange]);

  function update(next: TransformerConfig): void {
    form.setValue('config', convertTransformerConfigToForm(next), {
      shouldValidate: true,
    });
    onChange(
      convertJobMappingTransformerToForm(
        create(JobMappingTransformerSchema, { config: next })
      )
    );
  }

  const previewed = useDebouncedValue(value, 400);
  const previewConfig = toConfig(previewed);

  return (
    <>
      <section className="flex flex-col gap-3">
        <div className="flex flex-row items-center gap-2">
          <Label htmlFor="column-decision-transformer">Transformer</Label>
          <div className="grow" />
          {origin}
        </div>
        <TransformerSelect
          getTransformers={getTransformers}
          value={value}
          buttonText={getTransformerSelectButtonText(selected, placeholder)}
          buttonClassName="w-full"
          buttonTextClassName="grow"
          onSelect={(next) => {
            const nextConfig = toConfig(next);
            update(nextConfig ?? create(TransformerConfigSchema));
          }}
          disabled={disabled}
        />
        <Form {...form}>
          {/* Clé sur le seul transformer : suivre la config remonterait le
              formulaire à chaque frappe, et le champ perdrait le focus. */}
          <TransformerForm
            key={config?.config.case ?? 'none'}
            value={config ?? create(TransformerConfigSchema, {})}
            setValue={update}
            disabled={disabled}
            errors={form.formState.errors}
          />
        </Form>
      </section>

      {preview && (
        <section className="flex flex-col gap-3">
          <Label>Before &amp; after</Label>
          <ColumnPreview
            enabled
            connectionId={preview.connectionId}
            schema={preview.schema}
            table={preview.table}
            column={preview.column}
            transformer={
              previewConfig?.config.case === 'passthroughConfig'
                ? undefined
                : previewConfig
            }
            maxHeightClassName="max-h-72"
          />
        </section>
      )}
    </>
  );
}
