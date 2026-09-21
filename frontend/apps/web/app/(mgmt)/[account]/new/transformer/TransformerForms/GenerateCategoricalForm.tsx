'use client';
import { GenerateCategorical, GenerateCategoricalSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateCategorical> {}

export default function GenerateCategoricalForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateCategoricalSchema}
      options={[
        {
          field: 'categories',
          label: 'Categories',
          description:
            'Provide a list of comma-separated string values that you want to randomly select from.',
          stacked: true,
        },
      ]}
    />
  );
}
