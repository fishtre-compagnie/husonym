'use client';
import {
  GenerateStringPhoneNumber,
  GenerateStringPhoneNumberSchema,
} from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateStringPhoneNumber> {}

export default function GenerateStringPhoneNumberNumberForm(
  props: Props
): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateStringPhoneNumberSchema}
      options={[
        {
          field: 'min',
          label: 'Minimum Length',
          description:
            'Set the minimum length range of the output phone number.',
        },
        {
          field: 'max',
          label: 'Maximum Length',
          description:
            'Set the maximum length range of the output phone number.',
        },
      ]}
    />
  );
}
