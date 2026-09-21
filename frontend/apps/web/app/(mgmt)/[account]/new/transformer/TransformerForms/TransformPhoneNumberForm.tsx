'use client';
import { TransformPhoneNumber, TransformPhoneNumberSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformPhoneNumber> {}

export default function TransformPhoneNumberForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={TransformPhoneNumberSchema}
      options={[
        {
          field: 'preserveFormat',
          label: 'Preserve Format',
          description:
            'Keep the prefix (06, +33 6), separators and length of the input, and replace the other digits. Two distinct numbers never give the same output, and the same number always gives the same one.',
        },
        {
          field: 'preserveLength',
          label: 'Preserve Length',
          description:
            'Set the length of the output phone number to be the same as the input. Implied by Preserve Format.',
          disabledWhen: (value) => value.preserveFormat === true,
        },
      ]}
    />
  );
}
