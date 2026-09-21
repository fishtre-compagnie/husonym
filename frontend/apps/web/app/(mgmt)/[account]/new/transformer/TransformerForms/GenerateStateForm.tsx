'use client';
import { GenerateState, GenerateStateSchema } from '@husonym/sdk';
import { ReactElement } from 'react';
import OptionsForm from './options/OptionsForm';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<GenerateState> {}

export default function GenerateStateForm(props: Props): ReactElement {
  return (
    <OptionsForm
      {...props}
      schema={GenerateStateSchema}
      options={[
        {
          field: 'generateFullName',
          label: 'Generate Full Name',
          description:
            'Enable to return the full state name with a capitalized first letter. Returns the 2-letter state code by default.',
        },
      ]}
    />
  );
}
