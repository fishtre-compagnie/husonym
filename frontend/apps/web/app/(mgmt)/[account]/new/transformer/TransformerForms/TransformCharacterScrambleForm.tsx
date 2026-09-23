'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Badge } from '@/components/ui/badge';
import { CheckCircledIcon, CrossCircledIcon } from '@radix-ui/react-icons';

import LearnMoreLink from '@/components/labels/LearnMoreLink';
import { Button } from '@/components/ui/button';
import { useMutation } from '@connectrpc/connect-query';
import {
  TransformCharacterScramble,
  TransformCharacterScrambleSchema,
  TransformersService,
} from '@husonym/sdk';
import { ReactElement, useState } from 'react';
import OptionField from './options/OptionField';
import { TransformerConfigProps } from './util';

interface Props extends TransformerConfigProps<TransformCharacterScramble> {}

type ValidRegex = 'valid' | 'invalid' | 'null';

export default function TransformCharacterScrambleForm(
  props: Props
): ReactElement {
  const { value, setValue, isDisabled, errors } = props;

  const [isValidatingRegex, setIsValidatingRegex] = useState<boolean>(false);
  const [isRegexValid, setIsRegexValid] = useState<ValidRegex>('null');

  const { mutateAsync: validateUserRegexCodeAsync } = useMutation(
    TransformersService.method.validateUserRegexCode
  );

  async function handleValidateCode(): Promise<void> {
    setIsValidatingRegex(true);

    try {
      const res = await validateUserRegexCodeAsync({
        userProvidedRegex: value.userProvidedRegex,
      });
      setIsValidatingRegex(false);
      if (res.valid === true) {
        setIsRegexValid('valid');
      } else {
        setIsRegexValid('invalid');
      }
    } catch (err) {
      console.error(err);
      setIsValidatingRegex(false);
      setIsRegexValid('invalid');
    }
  }

  return (
    <div className="flex flex-col w-full space-y-4">
      <div className="flex flex-row gap-2 justify-end">
        {isRegexValid !== 'null' && (
          <Badge
            variant={isRegexValid === 'valid' ? 'success' : 'destructive'}
            className="h-9 px-4 py-2"
          >
            <ButtonText
              leftIcon={
                isRegexValid === 'valid' ? (
                  <CheckCircledIcon />
                ) : isRegexValid === 'invalid' ? (
                  <CrossCircledIcon />
                ) : null
              }
              text={isRegexValid === 'invalid' ? 'invalid' : 'valid'}
            />
          </Badge>
        )}
        <Button variant="secondary" type="button" onClick={handleValidateCode}>
          <ButtonText
            leftIcon={isValidatingRegex ? <Spinner /> : null}
            text={'Validate'}
          />
        </Button>
      </div>
      <OptionField
        schema={TransformCharacterScrambleSchema}
        value={value}
        setValue={setValue}
        isDisabled={isDisabled}
        errors={errors}
        field="userProvidedRegex"
        label="Regular Expression"
        description={
          <>
            Provide a Go regular expression to match and transform a substring
            of the value. Leave this blank to transform the entire value. Note:
            the regex needs to compile in Go.{' '}
            <LearnMoreLink href="https://docs.husonym.com/transformers/system#transform-character-scramble" />
          </>
        }
      />
    </div>
  );
}
