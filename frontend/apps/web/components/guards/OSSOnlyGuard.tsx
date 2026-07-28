import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { ReactElement } from 'react';

interface Props {
  children: ReactElement;
}

// Only renders children if the system is not Husonym Cloud
export default function OSSOnlyGuard(props: Props): ReactElement | null {
  const { children } = props;
  const { data: systemAppConfig } = useGetSystemAppConfig();

  if (systemAppConfig?.isHusonymCloud) {
    return null;
  }
  return children;
}
