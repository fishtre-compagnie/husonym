import { cn } from '@/libs/utils';
import { useValidationMessage } from '@/yup-validations/use-validation-message';
import { ReactElement } from 'react';

interface Props {
  message?: string;

  id?: string;
  className?: string;
}

export default function FormErrorMessage(props: Props): ReactElement | null {
  const { message, id, className } = props;
  const validationMessage = useValidationMessage();

  if (!message) {
    return null;
  }

  return (
    <p
      id={id}
      className={cn('text-[0.8rem] font-medium text-destructive', className)}
      {...props}
    >
      {validationMessage(message)}
    </p>
  );
}
