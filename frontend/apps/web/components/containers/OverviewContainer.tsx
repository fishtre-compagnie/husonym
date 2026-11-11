'use client';
import { cn } from '@/libs/utils';
import { ReactElement, ReactNode, createContext, useContext } from 'react';

interface Props {
  containerClassName?: string;
  Header: ReactNode;
  children: ReactNode;
  childrenStackClassnames?: string;
}

// Set by an OverviewContainer for what it holds: a page nested in a layout that is
// already one keeps the layout's column instead of indenting a second time.
const InsideOverview = createContext(false);

/** What it holds is not a page: an OverviewContainer in it keeps no page padding. */
export function NotAPage(props: { children: ReactNode }): ReactElement {
  return (
    <InsideOverview.Provider value={true}>
      {props.children}
    </InsideOverview.Provider>
  );
}

/**
 * The purpose of this component is to offer a standardized way of defining pages with a header.
 *
 * Outermost, it lays the page in the app's column (as PageBody does): centered, at most
 * 1240px wide, padded. Nested in another one, it only stacks its header and content.
 */
export default function OverviewContainer(props: Props): ReactElement {
  const { containerClassName, Header, children, childrenStackClassnames } =
    props;
  const nested = useContext(InsideOverview);

  return (
    <InsideOverview.Provider value={true}>
      <div
        className={cn(
          !nested && 'mx-auto w-full max-w-[1840px] px-4 py-8 sm:px-8',
          containerClassName
        )}
      >
        <div className={cn('header-container', nested ? 'my-6' : 'mb-7')}>
          {Header}
        </div>
        <div
          className={cn('flex', 'flex-col', 'gap-5', childrenStackClassnames)}
        >
          {children}
        </div>
      </div>
    </InsideOverview.Provider>
  );
}
