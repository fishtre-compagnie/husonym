import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useLicenseFeature } from '@/libs/hooks/useLicense';
import { isRoleSelectable } from '@/libs/license/license';
import { getAccountRoleString } from '@/util/util';
import { AccountRole } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement, useCallback } from 'react';

// The name of a role as the members pages show it.
export function useAccountRoleLabel(): (role: AccountRole) => string {
  const t = useTranslations('settings.members.roles');
  return useCallback(
    (role) => {
      switch (role) {
        case AccountRole.ADMIN:
          return t('admin');
        case AccountRole.JOB_DEVELOPER:
          return t('jobDeveloper');
        case AccountRole.JOB_EXECUTOR:
          return t('jobExecutor');
        case AccountRole.JOB_VIEWER:
          return t('jobViewer');
        default:
          // A role without a name of its own reads as the API names it.
          return getAccountRoleString(role);
      }
    },
    [t]
  );
}

interface Props {
  role: AccountRole;
  onChange(role: AccountRole): void;
}

export default function SelectAccountRole(props: Props): ReactElement {
  const { role, onChange } = props;
  const { allowed } = useLicenseFeature('rbac');
  const roleLabel = useAccountRoleLabel();

  return (
    <Select
      onValueChange={(newValue) => {
        if (newValue) {
          onChange(parseInt(newValue, 10) as AccountRole);
        }
      }}
      value={role.toString()}
    >
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {[
          AccountRole.ADMIN,
          AccountRole.JOB_DEVELOPER,
          AccountRole.JOB_EXECUTOR,
          AccountRole.JOB_VIEWER,
        ].map((role) => (
          <SelectItem
            key={role}
            className="cursor-pointer"
            value={role.toString()}
            disabled={!isRoleSelectable(allowed, role)}
          >
            {roleLabel(role)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
