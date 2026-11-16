import ButtonText from '@/components/ButtonText';
import FormErrorMessage from '@/components/FormErrorMessage';
import { getServerScope } from '@/components/connections/checks/targets';
import FormHeader from '@/components/forms/FormHeader';
import { PasswordInput } from '@/components/PasswordComponent';
import { PermissionConnectionType } from '@/components/permissions/columns';
import PermissionsDialog from '@/components/permissions/PermissionsDialog';
import { SecurePasswordInput } from '@/components/SecurePasswordInput';
import Spinner from '@/components/Spinner';
import SwitchCard from '@/components/switches/SwitchCard';
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { getErrorMessage } from '@/util/util';
import {
  AwsAdvancedFormValues,
  AwsCredentialsFormValues,
  ClientTlsFormValues,
  SqlOptionsFormValues,
  SshTunnelFormValues,
} from '@/yup-validations/connections';
import { create as createMessage } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import {
  CheckConnectionConfigByIdRequest,
  CheckConnectionConfigByIdResponse,
  CheckConnectionConfigRequest,
  CheckConnectionConfigResponse,
  CheckConnectionConfigResponseSchema,
  CheckSSHConnectionByIdRequest,
  CheckSSHConnectionRequest,
  CheckSSHConnectionResult,
  ConnectionRole,
  ConnectionService,
} from '@husonym/sdk';
import {
  CheckCircledIcon,
  ExclamationTriangleIcon,
} from '@radix-ui/react-icons';
import { ReactElement, useState } from 'react';
import { toast } from 'sonner';
import { useTranslations } from 'next-intl';

export interface SecretRevealProps<T> {
  isViewMode: boolean;
  canViewSecrets: boolean;
  onRevealClick(): Promise<T | undefined>;
}

interface NameProps {
  error?: string;
  value: string;
  onChange(value: string): void;
}

export function Name(props: NameProps): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const { error, value, onChange } = props;

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="name"
        title={t('name.title')}
        description={t('name.description')}
        isErrored={!!error}
        isRequired={true}
      />
      <Input
        id="name"
        autoCapitalize="off" // we don't allow capitals
        data-1p-ignore // tells 1password extension to not autofill this field
        value={value || ''}
        onChange={(e) => onChange(e.target.value)}
        placeholder={t('connectionName')}
      />
      <FormErrorMessage message={error} />
    </div>
  );
}

interface SqlConnectionOptionsProps {
  value: SqlOptionsFormValues;
  onChange(value: SqlOptionsFormValues): void;
  errors: Record<string, string>;
}

export function SqlConnectionOptions(
  props: SqlConnectionOptionsProps
): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const { value, onChange, errors } = props;

  return (
    <>
      <div className="space-y-2">
        <FormHeader
          htmlFor="maxConnectionLimit"
          title={t('maxConnectionLimit.title')}
          description={t('maxConnectionLimit.description')}
          isErrored={!!errors['options.maxConnectionLimit']}
        />
        <Input
          id="maxConnectionLimit"
          className="max-w-[180px]"
          type="number"
          value={value.maxConnectionLimit || ''}
          onChange={(e) =>
            onChange({
              ...value,
              maxConnectionLimit: e.target.valueAsNumber,
            })
          }
        />
        <FormErrorMessage message={errors['options.maxConnectionLimit']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="maxOpenDuration"
          title={t('maxOpenDuration.title')}
          description={t('maxOpenDuration.description')}
          isErrored={!!errors['options.maxOpenDuration']}
        />
        <Input
          id="maxOpenDuration"
          className="max-w-[180px]"
          value={value.maxOpenDuration || ''}
          onChange={(e) =>
            onChange({
              ...value,
              maxOpenDuration: e.target.value,
            })
          }
        />
        <FormErrorMessage message={errors['options.maxOpenDuration']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="maxIdleLimit"
          title={t('maxIdleLimit.title')}
          description={t('maxIdleLimit.description')}
          isErrored={!!errors['options.maxIdleLimit']}
        />
        <Input
          id="maxIdleLimit"
          className="max-w-[180px]"
          type="number"
          value={value.maxIdleLimit || ''}
          onChange={(e) =>
            onChange({
              ...value,
              maxIdleLimit: e.target.valueAsNumber,
            })
          }
        />
        <FormErrorMessage message={errors['options.maxIdleLimit']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="maxIdleDuration"
          title={t('maxIdleDuration.title')}
          description={t('maxIdleDuration.description')}
          isErrored={!!errors['options.maxIdleDuration']}
        />
        <Input
          id="maxIdleDuration"
          className="max-w-[180px]"
          value={value.maxIdleDuration || ''}
          onChange={(e) =>
            onChange({
              ...value,
              maxIdleDuration: e.target.value,
            })
          }
        />
        <FormErrorMessage message={errors['options.maxIdleDuration']} />
      </div>
    </>
  );
}

interface ClientTlsAccordionProps extends SecretRevealProps<ClientTlsFormValues> {
  value: ClientTlsFormValues;
  onChange(value: ClientTlsFormValues): void;
  errors: Record<string, string>;
}

export function ClientTlsAccordion(
  props: ClientTlsAccordionProps
): ReactElement {
  const t = useTranslations('connections.forms.shared');
  return (
    <Accordion type="single" collapsible className="w-full">
      <AccordionItem value="client-tls">
        <AccordionTrigger>{t('clientTls.title')}</AccordionTrigger>
        <AccordionContent className="flex flex-col gap-4 p-2">
          <div className="text-sm">{t('clientTls.description')}</div>
          <ClientTls {...props} />
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

interface ClientTlsProps extends SecretRevealProps<ClientTlsFormValues> {
  value: ClientTlsFormValues;
  onChange(value: ClientTlsFormValues): void;
  errors: Record<string, string>;
}

function ClientTls(props: ClientTlsProps): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const { value, onChange, errors, isViewMode, canViewSecrets, onRevealClick } =
    props;

  return (
    <>
      <div className="space-y-2">
        <FormHeader
          htmlFor="rootCert"
          title={t('rootCert.title')}
          description={t('rootCert.description')}
          isErrored={!!errors['clientTls.rootCert']}
        />
        <Textarea
          id="rootCert"
          value={value.rootCert || ''}
          onChange={(e) => onChange({ ...value, rootCert: e.target.value })}
        />
        <FormErrorMessage message={errors['clientTls.rootCert']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="clientCert"
          title={t('clientCert.title')}
          description={t('clientCert.description')}
          isErrored={!!errors['clientTls.clientCert']}
        />
        <Textarea
          id="clientCert"
          value={value.clientCert || ''}
          onChange={(e) => onChange({ ...value, clientCert: e.target.value })}
        />
        <FormErrorMessage message={errors['clientTls.clientCert']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="clientKey"
          title={t('clientKey.title')}
          description={t('clientKey.description')}
          isErrored={!!errors['clientTls.clientKey']}
        />
        {isViewMode ? (
          <SecurePasswordInput
            value={value.clientKey || ''}
            disabled={!canViewSecrets}
            onRevealPassword={
              canViewSecrets
                ? async () => {
                    const values = await onRevealClick();
                    return values?.clientKey ?? '';
                  }
                : undefined
            }
          />
        ) : (
          <Textarea
            id="clientKey"
            value={value.clientKey || ''}
            onChange={(e) => onChange({ ...value, clientKey: e.target.value })}
          />
        )}
        <FormErrorMessage message={errors['clientTls.clientKey']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="serverName"
          title={t('serverName.title')}
          description={t('serverName.description')}
          isErrored={!!errors['clientTls.serverName']}
        />
        <Textarea
          id="serverName"
          value={value.serverName || ''}
          onChange={(e) => onChange({ ...value, serverName: e.target.value })}
        />
        <FormErrorMessage message={errors['clientTls.serverName']} />
      </div>
    </>
  );
}

interface SshTunnelAccordionProps extends SecretRevealProps<SshTunnelFormValues> {
  value: SshTunnelFormValues;
  onChange(value: SshTunnelFormValues): void;
  errors: Record<string, string>;
  onCheckRequest(): CheckSSHConnectionRequest;
  onCheckIdRequest(): CheckSSHConnectionByIdRequest;
}

export function SshTunnelAccordion(
  props: SshTunnelAccordionProps
): ReactElement {
  const t = useTranslations('connections.forms.shared');
  return (
    <Accordion type="single" collapsible className="w-full">
      <AccordionItem value="ssh-tunnel">
        <AccordionTrigger>{t('sshTunnel.title')}</AccordionTrigger>
        <AccordionContent className="flex flex-col gap-4 p-2">
          <div className="text-sm">{t('sshTunnel.description')}</div>
          <SSHTunnel {...props} />
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

interface SSHTunnelProps extends SecretRevealProps<SshTunnelFormValues> {
  value: SshTunnelFormValues;
  onChange(value: SshTunnelFormValues): void;
  errors: Record<string, string>;
  onCheckRequest(): CheckSSHConnectionRequest;
  onCheckIdRequest(): CheckSSHConnectionByIdRequest;
}

function SSHTunnel(props: SSHTunnelProps): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const {
    value,
    onChange,
    errors,
    isViewMode,
    canViewSecrets,
    onRevealClick,
    onCheckRequest,
    onCheckIdRequest,
  } = props;

  return (
    <>
      <div className="space-y-2">
        <FormHeader
          htmlFor="host"
          title={t('host.title')}
          description={t('host.description')}
          isErrored={!!errors['tunnel.host']}
        />
        <Input
          id="host"
          value={value.host || ''}
          onChange={(e) => onChange({ ...value, host: e.target.value })}
        />
        <FormErrorMessage message={errors['tunnel.host']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="port"
          title={t('port.title')}
          description={t('port.description')}
          isErrored={!!errors['tunnel.port']}
        />
        <Input
          id="port"
          type="number"
          value={value.port || ''}
          onChange={(e) => onChange({ ...value, port: e.target.valueAsNumber })}
        />
        <FormErrorMessage message={errors['tunnel.port']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="user"
          title={t('user.title')}
          description={t('user.description')}
          isErrored={!!errors['tunnel.user']}
        />
        <Input
          id="user"
          value={value.user || ''}
          onChange={(e) => onChange({ ...value, user: e.target.value })}
        />
        <FormErrorMessage message={errors['tunnel.user']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="privateKey"
          title={t('privateKey.title')}
          description={t('privateKey.description')}
          isErrored={!!errors['tunnel.privateKey']}
        />
        {isViewMode ? (
          <SecurePasswordInput
            value={value.privateKey || ''}
            disabled={!canViewSecrets}
            onRevealPassword={
              canViewSecrets
                ? async () => {
                    const values = await onRevealClick();
                    return values?.privateKey ?? '';
                  }
                : undefined
            }
          />
        ) : (
          <Textarea
            id="privateKey"
            value={value.privateKey || ''}
            onChange={(e) => onChange({ ...value, privateKey: e.target.value })}
          />
        )}
        <FormErrorMessage message={errors['tunnel.privateKey']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="passphrase"
          title={t('passphrase.title')}
          description={t('passphrase.description')}
          isErrored={!!errors['tunnel.passphrase']}
        />
        {isViewMode ? (
          <SecurePasswordInput
            value={value.passphrase || ''}
            disabled={!canViewSecrets}
            onRevealPassword={
              canViewSecrets
                ? async () => {
                    const values = await onRevealClick();
                    return values?.passphrase ?? '';
                  }
                : undefined
            }
          />
        ) : (
          <Input
            id="passphrase"
            value={value.passphrase || ''}
            onChange={(e) => onChange({ ...value, passphrase: e.target.value })}
          />
        )}
        <FormErrorMessage message={errors['tunnel.passphrase']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="knownHostPublicKey"
          title={t('knownHostPublicKey.title')}
          description={t('knownHostPublicKey.description')}
          isErrored={!!errors['tunnel.knownHostPublicKey']}
        />
        <Input
          id="knownHostPublicKey"
          value={value.knownHostPublicKey || ''}
          onChange={(e) =>
            onChange({ ...value, knownHostPublicKey: e.target.value })
          }
          placeholder="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAlkjd9s7aJkfdLk3jSLkfj2lk3j2lkfj2l3kjf2lkfj2l"
        />
        <FormErrorMessage message={errors['tunnel.knownHostPublicKey']} />
        <div className="flex justify-end py-2">
          <CheckSSHConnectionButton
            onCheckRequest={onCheckRequest}
            onCheckIdRequest={onCheckIdRequest}
            isViewMode={isViewMode}
          />
        </div>
      </div>
    </>
  );
}

interface CheckSSHConnectionButtonProps {
  onCheckRequest(): CheckSSHConnectionRequest;
  onCheckIdRequest(): CheckSSHConnectionByIdRequest;
  isViewMode: boolean;
}

function CheckSSHConnectionButton(
  props: CheckSSHConnectionButtonProps
): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const { onCheckRequest, onCheckIdRequest, isViewMode } = props;
  const { mutateAsync: checkSSHConnection } = useMutation(
    ConnectionService.method.checkSSHConnection
  );
  const { mutateAsync: checkSSHConnectionById } = useMutation(
    ConnectionService.method.checkSSHConnectionById
  );
  const [isChecking, setIsChecking] = useState(false);
  const [checkResponse, setCheckResponse] = useState<
    CheckSSHConnectionResult | undefined
  >();

  async function onClick(): Promise<void> {
    if (isChecking) {
      return;
    }
    try {
      setCheckResponse(undefined);
      setIsChecking(true);
      if (isViewMode) {
        const res = await checkSSHConnectionById(onCheckIdRequest());
        setCheckResponse(res.result);
      } else {
        const res = await checkSSHConnection(onCheckRequest());
        setCheckResponse(res.result);
      }
    } catch (err) {
      console.error(err);
      toast.error(t('sshTunnelError'), {
        description: getErrorMessage(err),
      });
    } finally {
      setIsChecking(false);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-row gap-2 items-center">
        {checkResponse?.isSuccessful && (
          <CheckCircledIcon className="h-4 w-4 text-ok" />
        )}
        {checkResponse && !checkResponse.isSuccessful && (
          <ExclamationTriangleIcon className="h-4 w-4 text-danger" />
        )}

        <Button variant="outline" onClick={onClick} type="button">
          <ButtonText
            leftIcon={isChecking ? <Spinner /> : undefined}
            text={t('checkTunnelConnection')}
          />
        </Button>
      </div>
      {checkResponse && (
        <div>
          {checkResponse.errorMessage && (
            <ErrorAlert
              title={t('tunnelConnectionFailed')}
              description={checkResponse.errorMessage}
            />
          )}
        </div>
      )}
    </div>
  );
}

interface CheckConnectionButtonProps {
  isValid: boolean;
  connectionName: string;
  connectionType: PermissionConnectionType;
  mode: 'check' | 'checkById';
  getRequest(): CheckConnectionConfigRequest;
  getRequestById(): CheckConnectionConfigByIdRequest;
  // The connection can be tested in the role it plays in a job: MySQL and PostgreSQL.
  canCheckRole?: boolean;
}

export function CheckConnectionButton(
  props: CheckConnectionButtonProps
): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const {
    isValid,
    getRequest,
    getRequestById,
    connectionName,
    connectionType,
    mode,
    canCheckRole,
  } = props;
  const [isChecking, setIsChecking] = useState(false);
  const [role, setRole] = useState<ConnectionRole>(ConnectionRole.SOURCE);
  // The role the response was asked in: the choice may change after the test.
  const [checkedRole, setCheckedRole] = useState<ConnectionRole | undefined>();
  const [validationResponse, setValidationResponse] = useState<
    | CheckConnectionConfigResponse
    | CheckConnectionConfigByIdResponse
    | undefined
  >();
  const [openPermissionDialog, setOpenPermissionDialog] = useState(false);
  const { mutateAsync: checkConnectionConfig } = useMutation(
    ConnectionService.method.checkConnectionConfig
  );
  const { mutateAsync: checkConnectionConfigById } = useMutation(
    ConnectionService.method.checkConnectionConfigById
  );

  async function onClick(): Promise<void> {
    // Without tables, only what concerns the server as a whole is checked: a job's tables
    // are checked when the job is saved.
    const scope = canCheckRole ? getServerScope(role) : undefined;
    try {
      setIsChecking(true);
      setCheckedRole(scope ? role : undefined);
      if (mode === 'check') {
        const res = await checkConnectionConfig({ ...getRequest(), scope });
        setValidationResponse(res);
        setOpenPermissionDialog(!!res?.isConnected);
      } else {
        const res = await checkConnectionConfigById({
          ...getRequestById(),
          scope,
        });
        setValidationResponse(res);
        setOpenPermissionDialog(!!res?.isConnected);
      }
    } catch (err) {
      setValidationResponse(
        createMessage(CheckConnectionConfigResponseSchema, {
          isConnected: false,
          connectionError: getErrorMessage(err),
        })
      );
    } finally {
      setIsChecking(false);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <PermissionsDialog
        checkResponse={
          validationResponse ??
          createMessage(CheckConnectionConfigResponseSchema, {})
        }
        openPermissionDialog={openPermissionDialog}
        setOpenPermissionDialog={setOpenPermissionDialog}
        isValidating={isChecking}
        connectionName={connectionName}
        connectionType={connectionType}
        checkedRole={checkedRole}
      />
      <div className="flex flex-row items-center justify-end gap-3">
        {canCheckRole ? (
          <div className="flex flex-row items-center gap-2">
            <span className="text-sm text-muted-foreground">{t('testAs')}</span>
            <ToggleGroup
              type="single"
              value={String(role)}
              onValueChange={(value) => {
                // an empty value comes when the selected item is clicked again
                if (value) {
                  setRole(Number(value));
                }
              }}
            >
              <ToggleGroupItem
                className="border"
                value={String(ConnectionRole.SOURCE)}
              >
                {t('source')}
              </ToggleGroupItem>
              <ToggleGroupItem
                className="border"
                value={String(ConnectionRole.DESTINATION)}
              >
                {t('destination')}
              </ToggleGroupItem>
            </ToggleGroup>
          </div>
        ) : null}
        <Button
          variant="outline"
          disabled={!isValid}
          onClick={onClick}
          type="button"
        >
          <ButtonText
            leftIcon={isChecking ? <Spinner /> : undefined}
            text={t('testConnection')}
          />
        </Button>
      </div>
      {validationResponse && !validationResponse.isConnected && (
        <div>
          <ErrorAlert
            title={t('unableToConnect')}
            description={
              validationResponse.connectionError ?? t('noErrorReturned')
            }
          />
        </div>
      )}
    </div>
  );
}

interface ErrorAlertProps {
  title: string;
  description: string;
}

function ErrorAlert(props: ErrorAlertProps): ReactElement {
  const { title, description } = props;
  return (
    <Alert variant="destructive">
      <ExclamationTriangleIcon className="h-4 w-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
    </Alert>
  );
}

interface AwsAdvancedConfigAccordionProps {
  value: AwsAdvancedFormValues;
  onChange(value: AwsAdvancedFormValues): void;
  errors: Record<string, string>;
}

export function AwsAdvancedConfigAccordion(
  props: AwsAdvancedConfigAccordionProps
): ReactElement {
  const t = useTranslations('connections.forms.shared');
  return (
    <Accordion type="single" collapsible className="w-full">
      <AccordionItem value="advanced">
        <AccordionTrigger>{t('awsAdvanced.title')}</AccordionTrigger>
        <AccordionContent className="flex flex-col gap-4 p-2">
          <p className="text-sm">{t('awsAdvanced.description')}</p>
          <AwsAdvancedConfig {...props} />
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

interface AwsAdvancedConfigProps {
  value: AwsAdvancedFormValues;
  onChange(value: AwsAdvancedFormValues): void;
  errors: Record<string, string>;
}

function AwsAdvancedConfig(props: AwsAdvancedConfigProps): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const { value, onChange, errors } = props;

  return (
    <>
      <div className="space-y-2">
        <FormHeader
          htmlFor="region"
          title={t('region.title')}
          description={t('region.description')}
          isErrored={!!errors['advanced.region']}
        />
        <Input
          id="region"
          value={value.region || ''}
          onChange={(e) => onChange({ ...value, region: e.target.value })}
        />
        <FormErrorMessage message={errors['advanced.region']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="endpoint"
          title={t('endpoint.title')}
          description={t('endpoint.description')}
          isErrored={!!errors['advanced.endpoint']}
        />
        <Input
          id="endpoint"
          value={value.endpoint || ''}
          onChange={(e) => onChange({ ...value, endpoint: e.target.value })}
        />
        <FormErrorMessage message={errors['advanced.endpoint']} />
      </div>
    </>
  );
}

interface AwsCredentialsFormAccordionProps extends SecretRevealProps<AwsCredentialsFormValues> {
  value: AwsCredentialsFormValues;
  onChange(value: AwsCredentialsFormValues): void;
  errors: Record<string, string>;
}

export function AwsCredentialsFormAccordion(
  props: AwsCredentialsFormAccordionProps
): ReactElement {
  const t = useTranslations('connections.forms.shared');
  return (
    <Accordion type="single" collapsible className="w-full">
      <AccordionItem value="credentials">
        <AccordionTrigger>{t('awsCredentials')}</AccordionTrigger>
        <AccordionContent className="flex flex-col gap-4 p-2">
          <AwsCredentialsForm {...props} />
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}

interface AwsCredentialsFormProps extends SecretRevealProps<AwsCredentialsFormValues> {
  value: AwsCredentialsFormValues;
  onChange(value: AwsCredentialsFormValues): void;
  errors: Record<string, string>;
}

function AwsCredentialsForm(props: AwsCredentialsFormProps): ReactElement {
  const t = useTranslations('connections.forms.shared');
  const { value, onChange, errors, isViewMode, canViewSecrets, onRevealClick } =
    props;

  return (
    <>
      <div className="space-y-2">
        <FormHeader
          htmlFor="accessKeyId"
          title={t('accessKeyId.title')}
          description={t('accessKeyId.description')}
          isErrored={!!errors['credentials.accessKeyId']}
        />
        <Input
          id="accessKeyId"
          value={value.accessKeyId || ''}
          onChange={(e) => onChange({ ...value, accessKeyId: e.target.value })}
        />
        <FormErrorMessage message={errors['credentials.accessKeyId']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="secretAccessKey"
          title={t('secretAccessKey.title')}
          description={t('secretAccessKey.description')}
          isErrored={!!errors['credentials.secretAccessKey']}
        />
        {isViewMode ? (
          <SecurePasswordInput
            value={value.secretAccessKey || ''}
            disabled={!canViewSecrets}
            onRevealPassword={
              canViewSecrets
                ? async () => {
                    const values = await onRevealClick();
                    return values?.secretAccessKey ?? '';
                  }
                : undefined
            }
          />
        ) : (
          <PasswordInput
            id="secretAccessKey"
            value={value.secretAccessKey || ''}
            onChange={(e) =>
              onChange({ ...value, secretAccessKey: e.target.value })
            }
          />
        )}
        <FormErrorMessage message={errors['credentials.secretAccessKey']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="sessionToken"
          title={t('sessionToken.title')}
          description={t('sessionToken.description')}
          isErrored={!!errors['credentials.sessionToken']}
        />
        <Input
          id="sessionToken"
          value={value.sessionToken || ''}
          onChange={(e) => onChange({ ...value, sessionToken: e.target.value })}
        />
        <FormErrorMessage message={errors['credentials.sessionToken']} />
      </div>
      <div className="space-y-2">
        <SwitchCard
          isChecked={value.fromEc2Role ?? false}
          title={t('fromEc2Role')}
          description={t('ifTrueTheSdk')}
          onCheckedChange={(checked) =>
            onChange({ ...value, fromEc2Role: checked })
          }
        />
        <FormErrorMessage message={errors['credentials.fromEc2Role']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="roleArn"
          title={t('roleArn.title')}
          description={t('roleArn.description')}
          isErrored={!!errors['credentials.roleArn']}
        />
        <Input
          id="roleArn"
          value={value.roleArn || ''}
          onChange={(e) => onChange({ ...value, roleArn: e.target.value })}
        />
        <FormErrorMessage message={errors['credentials.roleArn']} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="roleExternalId"
          title={t('roleExternalId.title')}
          description={t('roleExternalId.description')}
          isErrored={!!errors['credentials.roleExternalId']}
        />
        <Input
          id="roleExternalId"
          value={value.roleExternalId || ''}
          onChange={(e) =>
            onChange({ ...value, roleExternalId: e.target.value })
          }
        />
        <FormErrorMessage message={errors['credentials.roleExternalId']} />
      </div>
    </>
  );
}
