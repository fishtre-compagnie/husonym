'use client';
import { Switch } from '@/components/ui/switch';
import { Permission } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

type PermissionsTranslator = ReturnType<
  typeof useTranslations<'settings.apiKeys.permissions'>
>;

interface PermissionOption {
  value: Permission;
  label:
    | 'view'
    | 'create'
    | 'edit'
    | 'run'
    | 'delete'
    | 'seeSecrets'
    | 'createAccounts';
  hint?: 'jobEdit' | 'jobRun' | 'connectionSecrets' | 'accountEdit';
}

interface PermissionGroup {
  title: 'jobs' | 'connections' | 'account';
  options: PermissionOption[];
}

// Every permission, by what it applies to. A key can do nothing that is not checked here.
const PERMISSION_GROUPS: PermissionGroup[] = [
  {
    title: 'jobs',
    options: [
      { value: Permission.JOB_VIEW, label: 'view' },
      { value: Permission.JOB_CREATE, label: 'create' },
      {
        value: Permission.JOB_EDIT,
        label: 'edit',
        hint: 'jobEdit',
      },
      {
        value: Permission.JOB_EXECUTE,
        label: 'run',
        hint: 'jobRun',
      },
      { value: Permission.JOB_DELETE, label: 'delete' },
    ],
  },
  {
    title: 'connections',
    options: [
      { value: Permission.CONNECTION_VIEW, label: 'view' },
      {
        value: Permission.CONNECTION_VIEW_SENSITIVE,
        label: 'seeSecrets',
        hint: 'connectionSecrets',
      },
      { value: Permission.CONNECTION_CREATE, label: 'create' },
      { value: Permission.CONNECTION_EDIT, label: 'edit' },
      { value: Permission.CONNECTION_DELETE, label: 'delete' },
    ],
  },
  {
    title: 'account',
    options: [
      { value: Permission.ACCOUNT_VIEW, label: 'view' },
      {
        value: Permission.ACCOUNT_EDIT,
        label: 'edit',
        hint: 'accountEdit',
      },
      { value: Permission.ACCOUNT_CREATE, label: 'createAccounts' },
      { value: Permission.ACCOUNT_DELETE, label: 'delete' },
    ],
  },
];

// The name of a permission as a person reads it, such as "Jobs: Run".
export function permissionLabel(
  permission: Permission,
  t: PermissionsTranslator
): string {
  for (const group of PERMISSION_GROUPS) {
    const option = group.options.find((o) => o.value === permission);
    if (option) {
      return t('label', {
        group: t(`groups.${group.title}`),
        option: t(`options.${option.label}`),
      });
    }
  }
  return t('unknown');
}

interface Props {
  value: Permission[];
  onChange(value: Permission[]): void;
}

export function PermissionsField(props: Props): ReactElement {
  const { value, onChange } = props;
  const t = useTranslations('settings.apiKeys.permissions');
  const toggle = (permission: Permission, checked: boolean): void => {
    onChange(
      checked ? [...value, permission] : value.filter((p) => p !== permission)
    );
  };
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      {PERMISSION_GROUPS.map((group) => (
        <div key={group.title} className="flex flex-col gap-2">
          <p className="text-sm font-medium">{t(`groups.${group.title}`)}</p>
          {group.options.map((option) => {
            const id = `permission-${option.value}`;
            return (
              <div key={option.value} className="flex flex-row gap-2">
                <Switch
                  id={id}
                  checked={value.includes(option.value)}
                  onCheckedChange={(checked) => toggle(option.value, checked)}
                />
                <label htmlFor={id} className="flex flex-col text-sm">
                  <span>{t(`options.${option.label}`)}</span>
                  {option.hint && (
                    <span className="text-xs text-muted-foreground">
                      {t(`hints.${option.hint}`)}
                    </span>
                  )}
                </label>
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}
