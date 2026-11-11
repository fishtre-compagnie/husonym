import Spinner from '@/components/Spinner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  CheckCircledIcon,
  CheckIcon,
  ExclamationTriangleIcon,
  ReloadIcon,
} from '@radix-ui/react-icons';
import { ReactElement } from 'react';

export type ErrorLevel = 'error' | 'warning';

export interface FormError {
  message: string;
  type?: string;
  path: string;
  level: ErrorLevel;
}

interface Props {
  formErrors: FormError[];
  isValidating?: boolean;
  onValidate?(): void;
}

export default function FormErrorsCard(props: Props): ReactElement {
  const { formErrors, isValidating, onValidate } = props;

  const { errors, warnings } = formErrorsToMessages(formErrors);
  return (
    <Card className="w-full flex flex-col">
      <CardHeader className="flex flex-col">
        <div className="flex flex-row items-center justify-between h-8">
          <div className="flex flex-row items-center gap-2">
            {errors.length != 0 ? (
              <ExclamationTriangleIcon className="h-4 w-4 text-danger-fg" />
            ) : (
              <CheckCircledIcon className="h-4 w-4 text-ok" />
            )}
            <CardTitle>Validations</CardTitle>
            {errors.length != 0 && (
              <Badge variant="destructive">
                {errors.length == 1
                  ? `${errors.length} Error`
                  : `${errors.length} Errors`}
              </Badge>
            )}
            {warnings.length != 0 && (
              <Badge className="border-transparent bg-warn-soft text-warn-fg">
                {warnings.length == 1
                  ? `${warnings.length} Warning`
                  : `${warnings.length} Warnings`}
              </Badge>
            )}
          </div>
          <div className="flex">
            {onValidate && (
              <Button
                variant="ghost"
                className="size-8"
                size="icon"
                key="validate"
                type="button"
                aria-label="Validate the mapping again"
                disabled={isValidating}
                onClick={() => onValidate()}
              >
                {isValidating ? (
                  <Spinner className="h-4 w-4" />
                ) : (
                  <ReloadIcon className="h-4 w-4" />
                )}
              </Button>
            )}
          </div>
        </div>
        <CardDescription>
          A list of schema validation errors to resolve before moving forward.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col flex-1">
        {formErrors.length === 0 && warnings.length === 0 ? (
          <div className="flex flex-col flex-1 items-center justify-center rounded-xl bg-ok-soft py-4 text-ok">
            <div className="text-sm flex flex-row items-center gap-2 px-1">
              <div className="flex">
                <CheckIcon />
              </div>
              <p>Everything looks good!</p>
            </div>
          </div>
        ) : (
          <ScrollArea className="max-h-[177px] overflow-auto">
            <div className="flex flex-col gap-2">
              {errors.map((message, index) => (
                <div
                  key={message + index}
                  className="text-wrap rounded-lg bg-danger-soft p-2 text-xs text-danger-fg"
                >
                  {message}
                </div>
              ))}
              {warnings.map((message, index) => (
                <div
                  key={message + index}
                  className="text-wrap rounded-lg bg-warn-soft p-2 text-xs text-warn-fg"
                >
                  {message}
                </div>
              ))}
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  );
}

interface FormErrorMesageResponse {
  errors: string[];
  warnings: string[];
}

function formErrorsToMessages(
  formErrors: FormError[]
): FormErrorMesageResponse {
  const errors: string[] = [];
  const warnings: string[] = [];
  formErrors.forEach((error) => {
    const pieces: string[] = [error.path];
    if (error.type) {
      pieces.push(`[${error.type}]`);
    }
    pieces.push(error.message);

    if (error.level == 'warning') {
      warnings.push(pieces.join(' '));
      return;
    }
    errors.push(pieces.join(' '));
  });

  return { errors, warnings };
}
