'use client';
import { GenerateGender, GenerateGenderSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateGender> {}

export default function GenerateGenderForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateGenderSchema}
      options={[
        {
          field: 'abbreviate',
          label: 'Abbreviate',
          description:
            'Abbreviate the gender to a single character. For example, female would be returned as f.',
        },
      ]}
    />
  );
}
