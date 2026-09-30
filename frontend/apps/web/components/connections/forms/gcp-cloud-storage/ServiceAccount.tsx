import FormErrorMessage from '@/components/FormErrorMessage';
import FormHeader from '@/components/forms/FormHeader';
import { SecurePasswordInput } from '@/components/SecurePasswordInput';
import { Textarea } from '@/components/ui/textarea';
import { ReactElement } from 'react';

interface Props {
  value: string;
  onChange(value: string): void;
  error?: string;

  isViewMode?: boolean;
  canViewSecrets?: boolean;
  onRevealPassword?(): Promise<string>;
}

// The key of the service account the connection acts with. Without one, the deployment must
// allow connections to act with the servers' own identity.
export default function ServiceAccount(props: Props): ReactElement {
  const {
    value,
    onChange,
    error,
    isViewMode,
    canViewSecrets,
    onRevealPassword,
  } = props;

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="serviceAccountCredentials"
        title="Service Account Key"
        description="The JSON key file of the service account the connection acts with. Without one, the connection acts with the identity of the servers running Husonym, which the deployment must allow."
        isErrored={!!error}
      />
      {isViewMode ? (
        <SecurePasswordInput
          id="serviceAccountCredentials"
          value={value || ''}
          disabled={!canViewSecrets}
          onRevealPassword={canViewSecrets ? onRevealPassword : undefined}
          placeholder='{"type": "service_account", ...}'
        />
      ) : (
        <Textarea
          id="serviceAccountCredentials"
          className="font-mono"
          rows={6}
          autoCapitalize="off"
          spellCheck={false}
          data-1p-ignore // tells 1password extension to not autofill this field
          value={value || ''}
          onChange={(e) => onChange(e.target.value)}
          placeholder='{"type": "service_account", ...}'
        />
      )}
      <FormErrorMessage message={error} />
    </div>
  );
}
