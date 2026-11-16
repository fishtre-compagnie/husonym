'use client';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useLocalePreference } from '@/components/providers/locale-provider';
import { LOCALES, Locale, LocalePreference, toPreference } from '@/i18n/config';
import { getAvatarFallback } from '@/libs/utils';
import { GearIcon } from '@radix-ui/react-icons';
import { signOutEverywhere } from '@/libs/sign-out';
import { useSession } from 'next-auth/react';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { toast } from 'sonner';
import { getErrorMessage } from '@/util/util';

// Each language in its own name, whatever the UI is in.
const LANGUAGE_NAMES: Record<Locale, string> = {
  en: 'English',
  fr: 'Français',
};

interface Props {
  // 'row': the sidebar's foot, the avatar beside the name and address.
  variant?: 'avatar' | 'row';
  // Without auth there is no session, but the menu still holds the language.
  isAuthEnabled?: boolean;
}

export function UserNav(props: Props = {}): ReactElement | null {
  const { variant = 'avatar', isAuthEnabled = true } = props;
  const session = useSession();
  const t = useTranslations('userMenu');

  const avatarImageSrc = session.data?.user?.image ?? '';
  const avatarImageAlt = session.data?.user?.name ?? 'unknown';
  const userName = session.data?.user?.name;
  const avatarFallback = userName ? getAvatarFallback(userName) : <GearIcon />;

  if (session.status === 'unauthenticated' && isAuthEnabled) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        {variant === 'row' ? (
          <button
            type="button"
            aria-label={t('trigger')}
            className="flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-lg p-2.5 text-left transition-colors hover:bg-hover"
          >
            <Avatar className="size-7">
              <AvatarImage src={avatarImageSrc} alt={avatarImageAlt} />
              <AvatarFallback>{avatarFallback}</AvatarFallback>
            </Avatar>
            <span className="flex min-w-0 flex-col text-[13px]">
              <span className="truncate text-foreground">
                {userName ?? (isAuthEnabled ? t('signedIn') : t('anonymous'))}
              </span>
              {session.data?.user?.email && (
                <span className="truncate text-xs text-muted-foreground">
                  {session.data.user.email}
                </span>
              )}
            </span>
          </button>
        ) : (
          <Button variant="ghost" className="relative h-8 w-8 rounded-full">
            <Avatar className="h-8 w-8">
              <AvatarImage src={avatarImageSrc} alt={avatarImageAlt} />
              <AvatarFallback>{avatarFallback}</AvatarFallback>
            </Avatar>
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent
        className="w-56"
        align={variant === 'row' ? 'start' : 'end'}
        side={variant === 'row' ? 'top' : undefined}
        forceMount
      >
        {(session.data?.user?.email || session.data?.user?.name) && (
          <div>
            <DropdownMenuLabel className="font-normal">
              <div className="flex flex-col space-y-1">
                {session.data?.user?.name && (
                  <p className="text-sm font-medium leading-none">
                    {session.data.user.name}
                  </p>
                )}
                {session.data?.user?.email && (
                  <p className="text-xs leading-none text-muted-foreground">
                    {session.data.user.email}
                  </p>
                )}
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
          </div>
        )}
        <DropdownMenuGroup>
          <DropdownMenuItem disabled>{t('profile')}</DropdownMenuItem>
          <DropdownMenuItem disabled>{t('settings')}</DropdownMenuItem>
          <LanguageSubmenu />
        </DropdownMenuGroup>
        {session.status === 'authenticated' && <DropdownMenuSeparator />}
        {session.status === 'authenticated' && (
          <DropdownMenuItem
            className="cursor-pointer"
            onClick={async () => {
              try {
                await signOutEverywhere();
              } catch (err) {
                console.error(err);
                toast.error(t('logOutError'));
              }
            }}
          >
            {t('logOut')}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// The UI's language, applied as soon as it is picked. "Default" follows the instance's
// language, and keeps following it should that change; "English" stays English.
function LanguageSubmenu(): ReactElement {
  const t = useTranslations('userMenu');
  const { preference, setPreference, isSaving } = useLocalePreference();

  async function onChange(value: string): Promise<void> {
    const next: LocalePreference = toPreference(value);
    if (next === preference) {
      return;
    }
    try {
      await setPreference(next);
    } catch (err) {
      console.error(err);
      toast.error(t('languageError'), { description: getErrorMessage(err) });
    }
  }

  return (
    <DropdownMenuSub>
      <DropdownMenuSubTrigger className="cursor-pointer">
        {t('language')}
      </DropdownMenuSubTrigger>
      <DropdownMenuSubContent>
        <DropdownMenuRadioGroup value={preference} onValueChange={onChange}>
          <DropdownMenuRadioItem
            value=""
            disabled={isSaving}
            className="cursor-pointer"
            // Picking keeps the menu open, the language changes under the pointer.
            onSelect={(e) => e.preventDefault()}
          >
            {t('languageDefault')}
          </DropdownMenuRadioItem>
          {LOCALES.map((locale) => (
            <DropdownMenuRadioItem
              key={locale}
              value={locale}
              lang={locale}
              disabled={isSaving}
              className="cursor-pointer"
              onSelect={(e) => e.preventDefault()}
            >
              {LANGUAGE_NAMES[locale]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuSubContent>
    </DropdownMenuSub>
  );
}
