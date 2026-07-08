'use client';

import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import { MagicWandIcon } from '@radix-ui/react-icons';
import { ReactElement } from 'react';

interface Props {
  isDisabled: boolean;
  isLoading: boolean;
  onClick(): void;
}

export default function AiSuggestionsButton(props: Props): ReactElement {
  const { isDisabled, isLoading, onClick } = props;
  return (
    <Button
      variant="outline"
      type="button"
      disabled={isDisabled || isLoading}
      onClick={onClick}
    >
      <ButtonText
        leftIcon={
          isLoading ? (
            <Spinner className="h-3 w-3" />
          ) : (
            <MagicWandIcon className="h-3 w-3" />
          )
        }
        text="AI Suggestions"
      />
    </Button>
  );
}
