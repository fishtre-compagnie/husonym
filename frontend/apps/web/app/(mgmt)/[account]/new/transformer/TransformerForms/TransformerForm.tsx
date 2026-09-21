import {
  create,
  DescMessage,
  MessageInitShape,
  MessageShape,
} from '@bufbuild/protobuf';
import { TransformerConfig, TransformerConfigSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import { FieldErrors } from 'react-hook-form';
import GenerateJavascriptForm from './GenerateJavascriptForm';
import OptionsForm from './options/OptionsForm';
import { TRANSFORMER_OPTIONS } from './options/registry';
import TransformCharacterScrambleForm from './TransformCharacterScrambleForm';
import TransformJavascriptForm from './TransformJavascriptForm';

interface Props {
  value: TransformerConfig;
  setValue(newValue: TransformerConfig): void;
  disabled?: boolean;

  errors?: FieldErrors<TransformerConfig>;

  NoConfigComponent?: ReactElement;
}
// handles rendering custom transformer configs
export default function TransformerForm(props: Props): ReactElement {
  const { value, disabled, setValue, errors, NoConfigComponent } = props;
  const valConfig = value.config; // de-refs so that typescript is able to keep the conditional typing as it doesn't work well if you keep it on value itself

  switch (valConfig.case) {
    case 'transformJavascriptConfig':
      return (
        <TransformJavascriptForm
          value={valConfig.value}
          setValue={(newVal) =>
            setValue(
              create(TransformerConfigSchema, {
                config: { case: valConfig.case, value: newVal },
              })
            )
          }
          isDisabled={disabled}
          errors={errors?.config?.value}
        />
      );
    case 'transformCharacterScrambleConfig':
      return (
        <TransformCharacterScrambleForm
          value={valConfig.value}
          setValue={(newVal) =>
            setValue(
              create(TransformerConfigSchema, {
                config: { case: valConfig.case, value: newVal },
              })
            )
          }
          isDisabled={disabled}
          errors={errors?.config?.value}
        />
      );
    case 'generateJavascriptConfig':
      return (
        <GenerateJavascriptForm
          value={valConfig.value}
          setValue={(newVal) =>
            setValue(
              create(TransformerConfigSchema, {
                config: { case: valConfig.case, value: newVal },
              })
            )
          }
          isDisabled={disabled}
          errors={errors?.config?.value}
        />
      );
  }

  // Every other transformer with options is a plain list of fields, declared in the registry.
  const registered = valConfig.case
    ? TRANSFORMER_OPTIONS[valConfig.case]
    : undefined;
  if (!registered || !valConfig.case) {
    return NoConfigComponent ?? <div />;
  }
  const configCase = valConfig.case;
  return (
    <OptionsForm<DescMessage>
      schema={registered.schema}
      options={registered.options}
      value={valConfig.value as MessageShape<DescMessage>}
      setValue={(newVal) =>
        setValue(
          // The registry is keyed by the case, so the value is the message of that case.
          create(TransformerConfigSchema, {
            config: { case: configCase, value: newVal },
          } as MessageInitShape<typeof TransformerConfigSchema>)
        )
      }
      isDisabled={disabled}
      errors={errors?.config?.value as FieldErrors}
    />
  );
}
