'use client';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { getAvatarFallback } from '@/libs/utils';
import { GearIcon } from '@radix-ui/react-icons';
import { signOutEverywhere } from '@/libs/sign-out';
import { useSession } from 'next-auth/react';
import { ReactElement } from 'react';
import { toast } from 'sonner';

interface Props {
  // 'row': the sidebar's foot, the avatar beside the name and address.
  variant?: 'avatar' | 'row';
}

export function UserNav(props: Props = {}): ReactElement | null {
  const { variant = 'avatar' } = props;
  const session = useSession();

  const avatarImageSrc = session.data?.user?.image ?? '';
  const avatarImageAlt = session.data?.user?.name ?? 'unknown';
  const userName = session.data?.user?.name;
  const avatarFallback = userName ? getAvatarFallback(userName) : <GearIcon />;

  if (session.status === 'unauthenticated') {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        {variant === 'row' ? (
          <button
            type="button"
            aria-label="Account menu"
            className="flex w-full min-w-0 cursor-pointer items-center gap-2.5 rounded-lg p-2.5 text-left transition-colors hover:bg-hover"
          >
            <Avatar className="size-7">
              <AvatarImage src={avatarImageSrc} alt={avatarImageAlt} />
              <AvatarFallback>{avatarFallback}</AvatarFallback>
            </Avatar>
            <span className="flex min-w-0 flex-col text-[13px]">
              <span className="truncate text-foreground">
                {userName ?? 'Signed in'}
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
          <DropdownMenuItem disabled>Profile</DropdownMenuItem>
          <DropdownMenuItem disabled>Settings</DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        {session.status === 'authenticated' && (
          <DropdownMenuItem
            className="cursor-pointer"
            onClick={async () => {
              try {
                await signOutEverywhere();
              } catch (err) {
                console.error(err);
                toast.error('Unable to sign out of provider session');
              }
            }}
          >
            Log out
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
