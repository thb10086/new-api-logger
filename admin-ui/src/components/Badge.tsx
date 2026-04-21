import { ReactNode } from 'react'
import { cn } from '@/lib/utils'

type Variant = 'default' | 'success' | 'warning' | 'error' | 'info' | 'gray' | 'purple'

interface Props {
  variant?: Variant
  children: ReactNode
  className?: string
}

const variantClasses: Record<Variant, string> = {
  default: 'bg-slate-100 text-slate-700',
  success: 'bg-emerald-100 text-emerald-700',
  warning: 'bg-amber-100 text-amber-700',
  error: 'bg-red-100 text-red-700',
  info: 'bg-blue-100 text-blue-700',
  gray: 'bg-gray-200 text-gray-600',
  purple: 'bg-purple-100 text-purple-700',
}

export default function Badge({ variant = 'default', children, className }: Props) {
  return (
    <span
      className={cn(
        'inline-flex items-center px-2 py-0.5 text-xs font-medium rounded',
        variantClasses[variant],
        className,
      )}
    >
      {children}
    </span>
  )
}

/** 根据 HTTP status 自动选 variant */
export function StatusBadge({ status }: { status: number }) {
  if (status === 0) return <Badge variant="gray">N/A</Badge>
  if (status >= 200 && status < 300) return <Badge variant="success">{status}</Badge>
  if (status >= 400 && status < 500) return <Badge variant="warning">{status}</Badge>
  if (status >= 500) return <Badge variant="error">{status}</Badge>
  return <Badge>{status}</Badge>
}

/** 根据 provider 字符串选颜色 */
export function ProviderBadge({ provider }: { provider: string }) {
  const variantMap: Record<string, Variant> = {
    openai_chat: 'info',
    openai_responses: 'info',
    anthropic: 'warning',
    gemini: 'success',
  }
  const variant = variantMap[provider] || 'gray'
  return <Badge variant={variant}>{provider}</Badge>
}
