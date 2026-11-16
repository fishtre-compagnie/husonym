import { siteConfig } from '@/app/config/site';
import { ArrowTopRightIcon, GitHubLogoIcon } from '@radix-ui/react-icons';
import Link from 'next/link';
import { ReactElement } from 'react';
import { PiBookOpenText, PiEnvelopeSimple } from 'react-icons/pi';
import { useTranslations } from 'next-intl';

export default function SupportDrawer(): ReactElement {
  const t = useTranslations('header.supportLinks');
  const supportLinks = [
    {
      name: t('documentation'),
      href: siteConfig.links.docs,
      icon: <PiBookOpenText className="h-4 w-4" />,
      description: t('documentationDescription'),
    },
    {
      name: t('github'),
      href: siteConfig.links.github,
      icon: <GitHubLogoIcon />,
      description: t('githubDescription'),
    },
    {
      name: t('email'),
      href: siteConfig.links.support,
      icon: <PiEnvelopeSimple className="h-4 w-4" />,
      description: t('emailDescription'),
    },
  ];

  return (
    <div className="flex flex-col gap-3 pt-10">
      {supportLinks.map((link) => (
        <Link
          key={link.name}
          href={link.href}
          target="_blank"
          rel="noreferrer"
          className="border border-border bg-card hover:bg-hover rounded-xl px-8 py-2"
        >
          <div className="flex flex-row justify-between items-center">
            <div className="flex flex-col gap-1">
              <div className="flex flex-row items-center gap-2">
                <div>{link.icon}</div>
                <div>{link.name}</div>
              </div>
              <div className="text-sm text-muted-foreground">
                {link.description}
              </div>
            </div>
            <ArrowTopRightIcon />
          </div>
        </Link>
      ))}
    </div>
  );
}
