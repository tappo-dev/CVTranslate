import { palette } from "../theme";

// Inline copy of web/public/icon.svg so the video has no runtime asset dependency.
export const Logo: React.FC<{ size: number; idPrefix: string }> = ({ size, idPrefix }) => {
  const firstCutoutId = `${idPrefix}-first-cutout`;
  const secondCutoutId = `${idPrefix}-second-cutout`;
  const thirdCutoutId = `${idPrefix}-third-cutout`;
  return (
    <svg width={size} height={size} viewBox="0 0 32 32">
      <defs>
        <mask id={firstCutoutId} maskUnits="userSpaceOnUse" x="0" y="0" width="32" height="32">
          <rect width="32" height="32" fill="#fff" />
          <rect x="7" y="7" width="18" height="18" rx="4.5" fill="#000" stroke="#000" strokeWidth="2.6" />
        </mask>
        <mask id={secondCutoutId} maskUnits="userSpaceOnUse" x="0" y="0" width="32" height="32">
          <rect width="32" height="32" fill="#fff" />
          <rect x="12" y="12" width="18" height="18" rx="4.5" fill="#000" stroke="#000" strokeWidth="2.6" />
        </mask>
        <mask id={thirdCutoutId} maskUnits="userSpaceOnUse" x="0" y="0" width="32" height="32">
          <rect width="32" height="32" fill="#fff" />
          <path
            d="M24.37 18.17A4.4 4.4 0 1 0 24.37 23.83"
            fill="none"
            stroke="#000"
            strokeWidth="2.4"
            strokeLinecap="round"
          />
        </mask>
      </defs>
      <rect x="2" y="2" width="18" height="18" rx="4.5" fill={palette.iconLight} mask={`url(#${firstCutoutId})`} />
      <rect x="7" y="7" width="18" height="18" rx="4.5" fill={palette.iconMid} mask={`url(#${secondCutoutId})`} />
      <rect x="12" y="12" width="18" height="18" rx="4.5" fill={palette.iconDark} mask={`url(#${thirdCutoutId})`} />
      <path d="M26 1.5Q26.6 5.4 30.5 6Q26.6 6.6 26 10.5Q25.4 6.6 21.5 6Q25.4 5.4 26 1.5Z" fill={palette.spark} />
    </svg>
  );
};
