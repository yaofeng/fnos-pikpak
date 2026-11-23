// dateText renders a timestamp the way every list shows one: in the reader's
// locale, and an em dash when there is none. A value the browser cannot parse
// is printed as it arrived rather than as "Invalid Date".
export function dateText(value?: string): string {
  if (!value) return '—'
  const at = new Date(value)
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString()
}
