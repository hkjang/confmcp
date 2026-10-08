/**
 * BrandMark is the confmcp logo: a Confluence page guarded by a permission
 * shield. It is a plain inline SVG so it needs no asset loading and renders
 * identically in an air-gapped install.
 */
export function BrandMark({ size = 32, title = 'confmcp' }: { size?: number; title?: string }) {
  const id = 'confmcp-brand'
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" role="img" aria-label={title} xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id={`${id}-bg`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#4263eb" />
          <stop offset="0.6" stopColor="#3b5bdb" />
          <stop offset="1" stopColor="#1c7ed6" />
        </linearGradient>
        <linearGradient id={`${id}-sh`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#20c997" />
          <stop offset="1" stopColor="#0ca678" />
        </linearGradient>
      </defs>
      <rect x="2" y="2" width="60" height="60" rx="15" fill={`url(#${id}-bg)`} />
      <path
        d="M15 11.5h17.5l9.5 9.5v27.5a3.5 3.5 0 0 1-3.5 3.5H15a3.5 3.5 0 0 1-3.5-3.5V15a3.5 3.5 0 0 1 3.5-3.5z"
        fill="#fff"
      />
      <path d="M32.5 11.5v6.5a3 3 0 0 0 3 3H42z" fill="#bac8ff" />
      <rect x="16.5" y="25" width="17" height="3.2" rx="1.6" fill="#4263eb" />
      <rect x="16.5" y="31.5" width="12" height="3.2" rx="1.6" fill="#91a7ff" />
      <rect x="16.5" y="38" width="9" height="3.2" rx="1.6" fill="#91a7ff" />
      <path
        d="M43 30.5l11 4.2v7.6c0 6.7-4.6 11.6-11 13.4-6.4-1.8-11-6.7-11-13.4v-7.6z"
        fill={`url(#${id}-sh)`}
        stroke="#fff"
        strokeWidth="2.6"
        strokeLinejoin="round"
      />
      <path
        d="M38.2 43.2l3.4 3.4 6.3-6.9"
        fill="none"
        stroke="#fff"
        strokeWidth="2.9"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
