import { Alert, AlertTitle } from '@/components/ui/alert';
import { ReactElement } from 'react';

// What the pages of a job show in its place when the job cannot be read: it was
// deleted, or it is not a job of this account.
export default function JobNotFoundAlert(): ReactElement {
  return (
    <Alert variant="destructive">
      <AlertTitle>Error: Unable to retrieve job</AlertTitle>
    </Alert>
  );
}
