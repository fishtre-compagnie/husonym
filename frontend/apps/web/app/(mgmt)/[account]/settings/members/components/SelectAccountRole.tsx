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
import { ReactElement } from 'react';

interface Props {
  role: AccountRole;
  onChange(role: AccountRole): void;
}

export default function SelectAccountRole(props: Props): ReactElement {
  const { role, onChange } = props;
  const { allowed } = useLicenseFeature('rbac');

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
            {getAccountRoleString(role)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
