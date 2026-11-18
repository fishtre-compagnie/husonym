import FormErrorMessage from '@/components/FormErrorMessage';
import FormHeader from '@/components/forms/FormHeader';
import OSSOnlyGuard from '@/components/guards/OSSOnlyGuard';
import { PasswordInput } from '@/components/PasswordComponent';
import { SecurePasswordInput } from '@/components/SecurePasswordInput';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  ActiveConnectionTab,
  MYSQL_CONNECTION_PROTOCOLS,
  MysqlFormValues,
} from '@/yup-validations/connections';
import { ReactElement } from 'react';
import { SecretRevealProps } from '../SharedFormInputs';
import { useTranslations } from 'next-intl';

interface Props extends SecretRevealProps<MysqlFormValues> {
  dbValue: MysqlFormValues['db'];
  onDbValueChange(value: MysqlFormValues['db']): void;

  urlValue: MysqlFormValues['url'];
  onUrlValueChange(value: MysqlFormValues['url']): void;

  envVarValue: MysqlFormValues['envVar'];
  onEnvVarValueChange(value: MysqlFormValues['envVar']): void;

  errors: Record<string, string>;

  activeTab: ActiveConnectionTab;
  setActiveTab(tab: ActiveConnectionTab): void;
}

export default function DatabaseCredentials(props: Props): ReactElement {
  const {
    dbValue,
    onDbValueChange,
    urlValue,
    onUrlValueChange,
    envVarValue,
    onEnvVarValueChange,
    errors,
    isViewMode,
    canViewSecrets,
    onRevealClick,
    activeTab,
    setActiveTab,
  } = props;

  return (
    <div className="flex flex-col gap-4">
      <ActiveTabSelector activeTab={activeTab} setActiveTab={setActiveTab} />
      {activeTab === 'url' && (
        <UrlTab
          urlValue={urlValue}
          onUrlValueChange={onUrlValueChange}
          error={errors.url}
          isViewMode={isViewMode}
          canViewSecrets={canViewSecrets}
          onRevealClick={async () => {
            const values = await onRevealClick();
            return values?.url ?? '';
          }}
        />
      )}
      {activeTab === 'host' && (
        <HostTab
          dbValue={dbValue}
          onDbValueChange={onDbValueChange}
          errors={errors}
          isViewMode={isViewMode}
          canViewSecrets={canViewSecrets}
          onRevealClick={async () => {
            const values = await onRevealClick();
            return values?.db ?? {};
          }}
        />
      )}
      {activeTab === 'url-env' && (
        <UrlEnvTab
          envVarValue={envVarValue}
          onEnvVarValueChange={onEnvVarValueChange}
          error={errors.envVar}
        />
      )}
    </div>
  );
}

interface ActiveTabProps {
  activeTab: ActiveConnectionTab;
  setActiveTab(tab: ActiveConnectionTab): void;
}

function ActiveTabSelector(props: ActiveTabProps): ReactElement {
  const t = useTranslations('connections.forms.fields');
  const { activeTab, setActiveTab } = props;

  return (
    <RadioGroup
      defaultValue={activeTab}
      onValueChange={(e) => setActiveTab(e as ActiveConnectionTab)}
      value={activeTab}
    >
      <div className="flex flex-col md:flex-row gap-4">
        <div className="text-sm">{t('connectBy')}</div>
        <div className="flex items-center space-x-2">
          <RadioGroupItem value="url" id="r2" />
          <Label htmlFor="r2">{t('tabUrl')}</Label>
        </div>
        <div className="flex items-center space-x-2">
          <RadioGroupItem value="host" id="r1" />
          <Label htmlFor="r1">{t('tabHost')}</Label>
        </div>
        <OSSOnlyGuard>
          <div className="flex items-center space-x-2">
            <RadioGroupItem value="url-env" id="r3" />
            <Label htmlFor="r3">{t('tabEnvVar')}</Label>
          </div>
        </OSSOnlyGuard>
      </div>
    </RadioGroup>
  );
}

interface UrlTabProps extends SecretRevealProps<MysqlFormValues['url']> {
  urlValue: MysqlFormValues['url'];
  onUrlValueChange(value: MysqlFormValues['url']): void;
  error?: string;
}

function UrlTab(props: UrlTabProps): ReactElement {
  const t = useTranslations('connections.forms.fields');
  const {
    urlValue,
    onUrlValueChange,
    error,
    isViewMode,
    canViewSecrets,
    onRevealClick,
  } = props;

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="url"
        title={t('url.title')}
        description={t('url.description')}
        isErrored={!!error}
        isRequired={true}
      />
      {isViewMode ? (
        <SecurePasswordInput
          value={urlValue || ''}
          maskedValue={urlValue ?? ''}
          disabled={!canViewSecrets}
          onRevealPassword={
            canViewSecrets
              ? async () => {
                  const values = await onRevealClick();
                  return values ?? '';
                }
              : undefined
          }
        />
      ) : (
        <Input
          id="url"
          autoCapitalize="off"
          data-1p-ignore // tells 1password extension to not autofill this field
          value={urlValue || ''}
          onChange={(e) => onUrlValueChange(e.target.value)}
          placeholder="username:password@tcp(hostname:port)/database"
        />
      )}
      <FormErrorMessage message={error} />
    </div>
  );
}

interface UrlEnvTabProps {
  envVarValue: MysqlFormValues['envVar'];
  onEnvVarValueChange(value: MysqlFormValues['envVar']): void;
  error?: string;
}

function UrlEnvTab(props: UrlEnvTabProps): ReactElement {
  const t = useTranslations('connections.forms.fields');
  const { envVarValue, onEnvVarValueChange, error } = props;

  return (
    <div className="space-y-2">
      <FormHeader
        htmlFor="envVar"
        title={t('envVar.title')}
        description={t('envVar.description')}
        isErrored={!!error}
        isRequired={true}
      />
      <Input
        id="envVar"
        autoCapitalize="off"
        data-1p-ignore // tells 1password extension to not autofill this field
        value={envVarValue || ''}
        onChange={(e) => onEnvVarValueChange(e.target.value)}
        placeholder="USER_DEFINED_MYSQL_URL"
      />
      <FormErrorMessage message={error} />
    </div>
  );
}

interface HostTabProps extends SecretRevealProps<MysqlFormValues['db']> {
  dbValue: MysqlFormValues['db'];
  onDbValueChange(value: MysqlFormValues['db']): void;
  errors: Record<string, string>;
}

function HostTab(props: HostTabProps): ReactElement {
  const t = useTranslations('connections.forms.fields');
  const {
    dbValue,
    onDbValueChange,
    errors,
    isViewMode,
    canViewSecrets,
    onRevealClick,
  } = props;

  return (
    <>
      <div className="space-y-2">
        <FormHeader
          htmlFor="host"
          title={t('host.title')}
          description={t('host.description')}
          isErrored={!!errors.host}
          isRequired={true}
        />
        <Input
          id="host"
          autoCapitalize="off"
          data-1p-ignore // tells 1password extension to not autofill this field
          value={dbValue.host || ''}
          onChange={(e) =>
            onDbValueChange({ ...dbValue, host: e.target.value })
          }
          placeholder="localhost"
        />
        <FormErrorMessage message={errors.host} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="port"
          title={t('port.title')}
          description={t('port.description')}
          isErrored={!!errors.port}
          isRequired={true}
        />
        <Input
          id="port"
          type="number"
          value={dbValue.port || ''}
          onChange={(e) =>
            onDbValueChange({ ...dbValue, port: e.target.valueAsNumber })
          }
          placeholder="3306"
        />
        <FormErrorMessage message={errors.port} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="name"
          title={t('mysqlDatabase.title')}
          description={t('mysqlDatabase.description')}
          isErrored={!!errors.name}
          isRequired={false}
        />
        <Input
          id="name"
          value={dbValue.name || ''}
          onChange={(e) =>
            onDbValueChange({ ...dbValue, name: e.target.value })
          }
          placeholder={t('mysqlDatabase.placeholder')}
        />
        <FormErrorMessage message={errors.name} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="user"
          title={t('user.title')}
          description={t('user.description')}
          isErrored={!!errors.user}
          isRequired={true}
        />
        <Input
          id="user"
          value={dbValue.user || ''}
          onChange={(e) =>
            onDbValueChange({ ...dbValue, user: e.target.value })
          }
          placeholder="mysql"
        />
        <FormErrorMessage message={errors.user} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="password"
          title={t('password.title')}
          description={t('password.description')}
          isErrored={!!errors.pass}
          isRequired={true}
        />
        {isViewMode ? (
          <SecurePasswordInput
            value={dbValue.pass || ''}
            disabled={!canViewSecrets}
            onRevealPassword={
              canViewSecrets
                ? async () => {
                    const values = await onRevealClick();
                    return values?.pass ?? '';
                  }
                : undefined
            }
          />
        ) : (
          <PasswordInput
            id="password"
            value={dbValue.pass || ''}
            onChange={(e) =>
              onDbValueChange({ ...dbValue, pass: e.target.value })
            }
            placeholder="mysql"
          />
        )}

        <FormErrorMessage message={errors.pass} />
      </div>
      <div className="space-y-2">
        <FormHeader
          htmlFor="protocol"
          title={t('protocol.title')}
          description={t('protocol.description')}
          isErrored={!!errors.protocol}
          isRequired={true}
        />
        <Select
          onValueChange={(value) =>
            onDbValueChange({ ...dbValue, protocol: value })
          }
          value={dbValue.protocol || ''}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {MYSQL_CONNECTION_PROTOCOLS.map((mode) => (
              <SelectItem className="cursor-pointer" key={mode} value={mode}>
                {mode}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <FormErrorMessage message={errors.protocol} />
      </div>
    </>
  );
}
