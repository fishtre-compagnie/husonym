import { summarizeOptions } from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/options/summary';
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

// La colonne dont l'aperçu lit les valeurs. Sans elle — job generate, ou édition
// de plusieurs colonnes à la fois — la décision se prend sans aperçu.
export interface PreviewTarget {
  connectionId: string;
  schema: string;
  table: string;
  column: string;
}

interface Props {
  value: JobMappingTransformerForm;
  onChange(value: JobMappingTransformerForm): void;
  // Les transformers offerts pour cette colonne : l'appelant les filtre par type,
  // et sur la page Source par contraintes (clé, colonne générée, nullable).
  getTransformers(): TransformerResult;
  // Le transformer choisi, pour le libellé du sélecteur.
  selected: Transformer;
  disabled?: boolean;
  preview?: PreviewTarget;
  // Validité des options saisies, pour que le parent désactive son action.
  onValidChange?(valid: boolean): void;
  placeholder?: string;
  // Affiché à droite du libellé « Transformer » : d'où vient ce choix.
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

// Clé stable d'un choix : deux configs identiques la partagent, ce qui évite de
// relancer l'aperçu ou la validation quand seul l'objet a changé.
export function configKey(transformer: JobMappingTransformerForm): string {
  const config = toConfig(transformer);
  return config ? toJsonString(TransformerConfigSchema, config) : '';
}

// Le cœur d'une décision de colonne : le transformer, ses options et ce qu'ils font
// des valeurs. Les trois vont ensemble — choisir sans voir le résultat était le
// défaut du sélecteur et du crayon qu'il remplace.
//
// Les options sont validées comme sur la page d'un transformer (le code JS compris) :
// le parent en est averti par `onValidChange` et refuse d'enregistrer ce qui ne passe pas.
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
  // Le formulaire ne porte pas la valeur — le parent en est la source — mais il valide
  // les options et donne aux champs le contexte que leurs libellés lisent.
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

  const key = configKey(value);
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

  const summary = summarizeOptions(config);
  // L'aperçu suit les options une fois qu'elles se posent, pas à chaque frappe.
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
        {summary.length > 0 && (
          <p className="text-xs text-muted-foreground">{summary.join(' · ')}</p>
        )}
        <Form {...form}>
          <TransformerForm
            key={key}
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
