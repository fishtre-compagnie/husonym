'use client';
import { GenerateCardNumber, GenerateCardNumberSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateCardNumber> {}

export default function GenerateCardNumberForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateCardNumberSchema}
      options={[
        {
          field: 'validLuhn',
          label: 'Valid Luhn',
          description:
            'Generate a 16 digit card number that passes a luhn check.',
        },
      ]}
    />
  );
}
