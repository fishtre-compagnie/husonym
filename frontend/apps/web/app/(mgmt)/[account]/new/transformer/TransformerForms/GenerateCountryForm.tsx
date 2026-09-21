'use client';
import { GenerateCountry, GenerateCountrySchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateCountry> {}

export default function GenerateCountryForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateCountrySchema}
      options={[
        {
          field: 'generateFullName',
          label: 'Generate Full Name',
          description:
            'Enable to return the full country name otherwise it returns the 2-letter country code by default.',
        },
      ]}
    />
  );
}
