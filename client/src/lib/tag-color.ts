// Shared helpers so tag chips render with the exact same colors everywhere
// (Tags page preview, contact panel filters, chat header badges).

/** Returns "#000000" or "#ffffff" giving the most readable foreground for `hex`. */
export const tagFg = (hex: string): string => {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex || "");
  if (!m) return "#ffffff";
  const n = parseInt(m[1], 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  const l = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
  return l > 0.6 ? "#0a0a0a" : "#ffffff";
};

/** Inline style for a solid tag chip (used in TagsPage, ChatTagsManager, ChatView header). */
export const tagChipStyle = (color: string): React.CSSProperties => ({
  backgroundColor: color,
  color: tagFg(color),
});