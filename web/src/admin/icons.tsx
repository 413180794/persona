// Icons the admin pages need beyond ui/icons.tsx, in the same grammar: 24-unit viewBox, round caps, currentColor.
import { Icon } from "../ui/icons";

const P = {
  gauge: <g><path d="M12 14l3.5-3.5" /><path d="M3.5 18a9 9 0 1 1 17 0" /><circle cx="12" cy="14" r="1.2" fill="currentColor" stroke="none" /></g>,
  mesh: <g><circle cx="12" cy="5" r="2.5" /><circle cx="5" cy="18" r="2.5" /><circle cx="19" cy="18" r="2.5" /><path d="M10.7 7.2 6.3 15.8M13.3 7.2l4.4 8.6M7.5 18h9" /></g>,
  phone: <g><rect x="6" y="2" width="12" height="20" rx="2.5" /><path d="M11 18h2" /></g>,
  shield: <g><path d="M12 22s8-3.5 8-10V5l-8-3-8 3v7c0 6.5 8 10 8 10z" /><path d="m9 12 2 2 4-4" /></g>,
  link: <g><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" /><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" /></g>,
  trash: <g><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" /></g>,
  qr: <g><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><path d="M14 14h3v3M21 14v.01M14 21h.01M17.5 17.5V21H21v-3.5" /></g>,
  power: <g><path d="M12 2v10" /><path d="M18.4 6.6a9 9 0 1 1-12.8 0" /></g>,
  alert: <g><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" /><path d="M12 9v4M12 17h.01" /></g>,
  lock: <g><rect x="4" y="11" width="16" height="10" rx="2.5" /><path d="M8 11V7a4 4 0 0 1 8 0v4" /></g>,
  pulse: <path d="M22 12h-4l-3 9L9 3l-3 9H2" />,
  unlink: <g><path d="M18.84 12.25l1.72-1.71a5 5 0 0 0-7.07-7.07l-1.72 1.71M5.17 11.75l-1.71 1.71a5 5 0 0 0 7.07 7.07l1.71-1.71M8 2v3M2 8h3M16 22v-3M22 16h-3" /></g>,
  radar: <g><circle cx="12" cy="12" r="9" /><circle cx="12" cy="12" r="5" /><path d="M12 12l6-6" /><circle cx="12" cy="12" r="1" fill="currentColor" stroke="none" /></g>,
  pencil: <path d="M17 3a2.8 2.8 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5z" />,
  puzzle: <path d="M9 3.5a2 2 0 0 1 4 0V5h3.5A1.5 1.5 0 0 1 18 6.5V10h1.5a2 2 0 0 1 0 4H18v3.5a1.5 1.5 0 0 1-1.5 1.5H13v-1.5a2 2 0 0 0-4 0V19H5.5A1.5 1.5 0 0 1 4 17.5V14h1.5a2 2 0 0 0 0-4H4V6.5A1.5 1.5 0 0 1 5.5 5H9z" />,
  upload: <g><path d="M12 15V3M7 8l5-5 5 5" /><path d="M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4" /></g>,
} satisfies Record<string, React.ReactNode>;

const SHARED = ["machine", "copy", "plus", "x", "check", "retry", "key", "logout", "sun", "moon", "monitor", "menu", "web", "terminal", "chevronRight", "spark", "external", "info", "chevron", "undo", "up", "folder", "search", "git"] as const;

export type AdminIconName = keyof typeof P | (typeof SHARED)[number];

export function AdminIcon({ name, size = 15, stroke = 1.9, className, style }: { name: AdminIconName; size?: number; stroke?: number; className?: string; style?: React.CSSProperties }) {
  if (!(name in P)) return <Icon name={name} size={size} stroke={stroke} className={className} style={style} />;
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={stroke} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className={className} style={style}>
      {P[name as keyof typeof P]}
    </svg>
  );
}
