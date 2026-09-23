import type { SVGProps } from "react";

// One icon family for the whole console: 24-unit grid, 1.6 stroke, currentColor.
// Keeping them inline avoids a second font/network dependency and lets every
// icon inherit the surrounding text colour in both themes.
const PATHS = {
  server: "M4 5.5h16v5H4zM4 13.5h16v5H4zM7.5 8h.01M7.5 16h.01",
  tool: "M14.5 3.5a4.5 4.5 0 0 0-5.9 5.7L3.6 14.2a2 2 0 1 0 2.8 2.8l5-5a4.5 4.5 0 0 0 5.8-5.9l-2.6 2.6-2.1-2.1z",
  key: "M15 3a6 6 0 1 0-4.2 10.2L9 15H7v2H5v2H3v-3l7.2-7.2A6 6 0 0 0 15 3zM16.5 6.5h.01",
  shield: "M12 3.5 5 6.2v5c0 4.2 2.9 7.6 7 9.3 4.1-1.7 7-5.1 7-9.3v-5z",
  activity: "M3.5 12h3.2l2.3-6 3.6 12 2.4-6h5.5",
  users: "M8.5 11a3.2 3.2 0 1 0 0-6.5 3.2 3.2 0 0 0 0 6.5zM2.5 19.5a6 6 0 0 1 12 0M16 5a3.2 3.2 0 0 1 0 6.3M17.5 14.4a5.6 5.6 0 0 1 4 5.1",
  user: "M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 20a8 8 0 0 1 16 0",
  gauge: "M12 19a7 7 0 1 1 7-7M12 12l4-3",
  chart: "M4 20V9M10 20V4M16 20v-7M22 20H2",
  search: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20.5 20.5 16 16",
  refresh: "M20 7.5A8.5 8.5 0 1 0 21 13M20 3.5v4h-4",
  copy: "M9 9h10v11H9zM15 6H5v11h2",
  check: "M4.5 12.5 9.5 17.5 19.5 6.5",
  close: "M6 6l12 12M18 6 6 18",
  chevronRight: "M9 5l7 7-7 7",
  chevronDown: "M5 9l7 7 7-7",
  chevronLeft: "M15 5l-7 7 7 7",
  arrowUp: "M12 20V4M5.5 10.5 12 4l6.5 6.5",
  arrowDown: "M12 4v16M5.5 13.5 12 20l6.5-6.5",
  sortNone: "M8 10.5 12 6l4 4.5M8 13.5l4 4.5 4-4.5",
  external: "M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5",
  sun: "M12 5.5v-2M12 20.5v-2M5.5 12h-2M20.5 12h-2M7.2 7.2 5.8 5.8M18.2 18.2l-1.4-1.4M7.2 16.8l-1.4 1.4M18.2 5.8l-1.4 1.4M12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8z",
  moon: "M20 14.5A8.5 8.5 0 0 1 9.5 4 8.5 8.5 0 1 0 20 14.5z",
  menu: "M4 7h16M4 12h16M4 17h16",
  filter: "M4 5.5h16l-6.2 7.3V19l-3.6 1.6v-7.8z",
  alert: "M12 4 2.8 20h18.4zM12 10v4.5M12 17.5h.01",
  info: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5M12 7.8h.01",
  plus: "M12 5v14M5 12h14",
  trash: "M4.5 7h15M9.5 7V4.5h5V7M6.5 7l1 13h9l1-13M10.5 11v5M13.5 11v5",
  inbox: "M3.5 13.5h4l1.5 3h6l1.5-3h4M3.5 13.5 6 5h12l2.5 8.5V19h-17z",
  clock: "M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7.5V12l3 2",
  power: "M12 4v8M7.8 6.4a7.5 7.5 0 1 0 8.4 0",
  book: "M6 3.5h7l5 5v12H6zM13 3.5V9h5M9 13h6M9 16.5h6",
  logout: "M14 7V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1v-2M9.5 12H21M17.5 8.5 21 12l-3.5 3.5",
  login: "M10 7V5a1 1 0 0 1 1-1h8a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-8a1 1 0 0 1-1-1v-2M3 12h11M10.5 8.5 14 12l-3.5 3.5",
} as const;

export type IconName = keyof typeof PATHS;

type IconProps = SVGProps<SVGSVGElement> & {
  name: IconName;
  size?: number;
};

export function Icon({ name, size = 16, className, ...rest }: IconProps) {
  return (
    <svg
      className={className ? `icon ${className}` : "icon"}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...rest}
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
