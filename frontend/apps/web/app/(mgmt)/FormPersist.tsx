import { ReactElement } from 'react';
import { Control, FieldValues, UseFormReturn } from 'react-hook-form';
import useFormPersist from './useFormPersist';

// TContext suit celui du formulaire : un resolver est invariant sur son contexte,
// et le figer à unknown rejetait tout formulaire construit avec un contexte de
// validation (accountId, isJobNameAvailable…).
interface FormPersistProps<
  T extends FieldValues,
  TContext = unknown,
  TTransformedValues = T,
> {
  form: UseFormReturn<T, TContext, TTransformedValues>;
  formKey: string;
}
const isBrowser = () => typeof window !== 'undefined';

export default function FormPersist<
  T extends FieldValues,
  TContext = unknown,
  TTransformedValues = T,
>(props: FormPersistProps<T, TContext, TTransformedValues>): ReactElement {
  const { form, formKey } = props;
  useFormPersist(formKey, {
    // useFormPersist operates on string keys and is intentionally `any`-typed;
    // the form's concrete generics carry no extra safety here.
    control: form.control as Control<FieldValues>,
    setValue: form.setValue,
    storage: isBrowser() ? window.sessionStorage : undefined,
  });
  return <></>;
}
