import { clsx, type ClassValue } from 'clsx'

/** 合并 className (clsx 透传, 后续可换成 tailwind-merge) */
export function cn(...inputs: ClassValue[]): string {
  return clsx(inputs)
}

/** 字节数 → 人类可读 */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

/** 毫秒 → 人类可读 */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  return `${(ms / 60_000).toFixed(2)}min`
}

/** 数字千分位 */
export function formatNumber(n: number | null | undefined): string {
  if (n === null || n === undefined) return '-'
  return n.toLocaleString('en-US')
}

/** ISO 时间 → 本地化字符串 (yyyy-MM-dd HH:mm:ss) */
export function formatTime(iso: string | null | undefined): string {
  if (!iso) return '-'
  try {
    const d = new Date(iso)
    if (isNaN(d.getTime())) return iso
    const yyyy = d.getFullYear()
    const mm = String(d.getMonth() + 1).padStart(2, '0')
    const dd = String(d.getDate()).padStart(2, '0')
    const hh = String(d.getHours()).padStart(2, '0')
    const mi = String(d.getMinutes()).padStart(2, '0')
    const ss = String(d.getSeconds()).padStart(2, '0')
    return `${yyyy}-${mm}-${dd} ${hh}:${mi}:${ss}`
  } catch {
    return iso
  }
}

/** 截断长文本 */
export function truncate(s: string | null | undefined, n: number = 80): string {
  if (!s) return ''
  if (s.length <= n) return s
  return s.slice(0, n) + '…'
}
