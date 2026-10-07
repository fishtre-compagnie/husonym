import { useLicenseFeature } from '@/libs/hooks/useLicense';
import {
  featureLabel,
  featureNoticeMessage,
  LicenseFeature,
} from '@/libs/license/license';
import Link from 'next/link';
import { ReactElement } from 'react';
import { useAccount } from './providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from './ui/alert';

interface Props {
  feature: LicenseFeature;
  className?: string;
}

// Says why the actions of a feature are greyed out. Silent while the license allows the
// feature, or has not been read.
export default function LicenseFeatureNotice(
  props: Props
): ReactElement | null {
  const { feature, className } = props;
  const { allowed, license } = useLicenseFeature(feature);
  const { account } = useAccount();

  if (allowed) {
    return null;
  }

  return (
    <Alert variant="warning" className={className}>
      <AlertTitle>{featureLabel(feature)}</AlertTitle>
      <AlertDescription>
        {featureNoticeMessage(license)}{' '}
        <Link href={`/${account?.name}/settings/license`} className="underline">
          View license
        </Link>
      </AlertDescription>
    </Alert>
  );
}
