// "Add as context" has no icon of its own in the shared set yet: a quote mark, in the icons' grammar.
export function QuoteIcon({ size = 15 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.9} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 11h5v5H4zM4 11c0-3 1.5-5 4-6M14 11h5v5h-5zM14 11c0-3 1.5-5 4-6" />
    </svg>
  );
}
