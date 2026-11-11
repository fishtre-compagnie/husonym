import { ReactElement, ReactNode } from 'react';

// The stroke icons of the redesign's shell: 24px grid, drawn in currentColor.
function Stroke(props: {
  children: ReactNode;
  size?: number;
  className?: string;
  strokeWidth?: number;
}): ReactElement {
  const { children, size = 16, className, strokeWidth = 1.8 } = props;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      {children}
    </svg>
  );
}

type IconProps = { size?: number; className?: string };

export function DashboardIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <rect x="3" y="3" width="7" height="9" rx="1.5" />
      <rect x="14" y="3" width="7" height="5" rx="1.5" />
      <rect x="14" y="12" width="7" height="9" rx="1.5" />
      <rect x="3" y="16" width="7" height="5" rx="1.5" />
    </Stroke>
  );
}
export function JobsIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="M12 3 3 8l9 5 9-5-9-5Z" />
      <path d="m3 13 9 5 9-5" />
    </Stroke>
  );
}
export function RunsIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="M3 12h4l3-8 4 16 3-8h4" />
    </Stroke>
  );
}
export function ConnectionsIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <ellipse cx="12" cy="5" rx="8" ry="3" />
      <path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5" />
      <path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" />
    </Stroke>
  );
}
export function TransformersIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="m4 20 10-10" />
      <path d="m14 4 1 2 2 1-2 1-1 2-1-2-2-1 2-1 1-2Z" />
      <path d="M19 13v4M17 15h4" />
    </Stroke>
  );
}
export function ReviewIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="M12 3 4 6v6c0 4.5 3.4 8.3 8 9 4.6-.7 8-4.5 8-9V6l-8-3Z" />
      <path d="m9 12 2 2 4-4" />
    </Stroke>
  );
}
export function SettingsIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="M4 7h10M18 7h2M4 17h4M12 17h8" />
      <circle cx="16" cy="7" r="2" />
      <circle cx="10" cy="17" r="2" />
    </Stroke>
  );
}
export function ChevronDownIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="m6 9 6 6 6-6" />
    </Stroke>
  );
}
export function SearchIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </Stroke>
  );
}
export function SunIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </Stroke>
  );
}
export function MoonIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z" />
    </Stroke>
  );
}
export function BellIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="M6 16V11a6 6 0 1 1 12 0v5l1.5 2h-15L6 16Z" />
      <path d="M10 20a2 2 0 0 0 4 0" />
    </Stroke>
  );
}
export function HelpIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6" />
      <path d="M12 17h.01" />
    </Stroke>
  );
}
export function PlusIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p} strokeWidth={2}>
      <path d="M12 5v14M5 12h14" />
    </Stroke>
  );
}
export function MenuIcon(p: IconProps): ReactElement {
  return (
    <Stroke {...p}>
      <path d="M4 6h16M4 12h16M4 18h16" />
    </Stroke>
  );
}
