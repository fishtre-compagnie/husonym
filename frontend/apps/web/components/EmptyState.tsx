import { PlusIcon } from '@radix-ui/react-icons';
import NextLink from 'next/link';
import { ReactElement, ReactNode, type JSX } from 'react';
import ButtonText from './ButtonText';
import { Button } from './ui/button';

interface Props {
  title: string;
  description: string;
  icon: JSX.Element;

  extra?: ReactNode;
}

export default function EmptyState(props: Props): ReactElement {
  const { title, description, icon, extra } = props;
  return (
    <div className="flex min-h-[360px] flex-col items-center justify-center rounded-2xl border border-dashed border-border bg-card p-8 text-center">
      <div className="mb-4 flex size-14 items-center justify-center rounded-2xl bg-brand-soft text-link">
        {icon}
      </div>
      <h2 className="mb-2 font-display text-xl font-semibold text-hi">
        {title}
      </h2>
      <p className="mb-6 max-w-sm text-muted-foreground">{description}</p>
      {extra ? extra : null}
    </div>
  );
}

interface EmptyStateLinkButtonProps {
  href: string;
  buttonIconSide?: 'left' | 'right';
  buttonIcon?: ReactNode;
  buttonText: string;
}
export function EmptyStateLinkButton(
  props: EmptyStateLinkButtonProps
): ReactElement {
  const { buttonIconSide, href, buttonIcon = <PlusIcon />, buttonText } = props;
  return (
    <NextLink href={href}>
      <Button>
        <ButtonText
          rightIcon={buttonIconSide === 'right' ? buttonIcon : undefined}
          leftIcon={buttonIconSide === 'left' ? buttonIcon : undefined}
          text={buttonText}
        />
      </Button>
    </NextLink>
  );
}
