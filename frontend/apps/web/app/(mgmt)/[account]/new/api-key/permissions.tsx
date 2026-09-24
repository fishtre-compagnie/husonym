'use client';
import { Switch } from '@/components/ui/switch';
import { Permission } from '@husonym/sdk';
import { ReactElement } from 'react';

interface PermissionOption {
  value: Permission;
  label: string;
  hint?: string;
}

interface PermissionGroup {
  title: string;
  options: PermissionOption[];
}

// Every permission, by what it applies to. A key can do nothing that is not checked here.
const PERMISSION_GROUPS: PermissionGroup[] = [
  {
    title: 'Jobs',
    options: [
      { value: Permission.JOB_VIEW, label: 'View' },
      { value: Permission.JOB_CREATE, label: 'Create' },
      { value: Permission.JOB_EDIT, label: 'Edit' },
      {
        value: Permission.JOB_EXECUTE,
        label: 'Run',
        hint: 'A run writes to a real destination.',
      },
      { value: Permission.JOB_DELETE, label: 'Delete' },
    ],
  },
  {
    title: 'Connections',
    options: [
      { value: Permission.CONNECTION_VIEW, label: 'View' },
      {
        value: Permission.CONNECTION_VIEW_SENSITIVE,
        label: 'See secrets',
        hint: 'Passwords and keys in clear. Without it, they come back masked.',
      },
      { value: Permission.CONNECTION_CREATE, label: 'Create' },
      { value: Permission.CONNECTION_EDIT, label: 'Edit' },
      { value: Permission.CONNECTION_DELETE, label: 'Delete' },
    ],
  },
  {
    title: 'Account',
    options: [
      { value: Permission.ACCOUNT_VIEW, label: 'View' },
      {
        value: Permission.ACCOUNT_EDIT,
        label: 'Edit',
        hint: 'Settings, members and hooks.',
      },
      { value: Permission.ACCOUNT_CREATE, label: 'Create accounts' },
      { value: Permission.ACCOUNT_DELETE, label: 'Delete' },
    ],
  },
];

// The name of a permission as a person reads it, such as "Jobs: Run".
export function permissionLabel(permission: Permission): string {
  for (const group of PERMISSION_GROUPS) {
    const option = group.options.find((o) => o.value === permission);
    if (option) {
      return `${group.title}: ${option.label}`;
    }
  }
  return 'Unknown';
}

interface Props {
  value: Permission[];
  onChange(value: Permission[]): void;
}

export function PermissionsField(props: Props): ReactElement {
  const { value, onChange } = props;
  const toggle = (permission: Permission, checked: boolean): void => {
    onChange(
      checked ? [...value, permission] : value.filter((p) => p !== permission)
    );
  };
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      {PERMISSION_GROUPS.map((group) => (
        <div key={group.title} className="flex flex-col gap-2">
          <p className="text-sm font-medium">{group.title}</p>
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
                  <span>{option.label}</span>
                  {option.hint && (
                    <span className="text-xs text-muted-foreground">
                      {option.hint}
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
