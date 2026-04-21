/**
 * ClientTypeFilter - 客户端类型下拉过滤.
 *
 * 与 gateway/internal/handlers/client_classify.go 的枚举值保持一致.
 */

export const CLIENT_TYPES = [
  { value: '', label: '全部客户端' },
  { value: 'claude_code', label: 'Claude Code' },
  { value: 'anthropic_sdk', label: 'Anthropic SDK' },
  { value: 'openai_sdk', label: 'OpenAI SDK' },
  { value: 'langchain', label: 'LangChain' },
  { value: 'cherry_studio', label: 'Cherry Studio' },
  { value: 'chatbox', label: 'Chatbox' },
  { value: 'lobechat', label: 'LobeChat' },
  { value: 'cursor', label: 'Cursor' },
  { value: 'continue', label: 'Continue.dev' },
  { value: 'browser', label: '浏览器' },
  { value: 'curl', label: 'curl / Postman' },
  { value: 'unknown', label: '未识别' },
] as const

export type ClientTypeValue = (typeof CLIENT_TYPES)[number]['value']

export const CLIENT_TYPE_LABEL: Record<string, string> = CLIENT_TYPES.reduce(
  (acc, c) => {
    if (c.value) acc[c.value] = c.label
    return acc
  },
  {} as Record<string, string>,
)

interface Props {
  value: string
  onChange: (v: string) => void
  className?: string
}

export default function ClientTypeFilter({ value, onChange, className }: Props) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={
        className ||
        'px-3 py-1.5 text-sm border border-slate-300 rounded-md bg-white min-w-[10rem]'
      }
    >
      {CLIENT_TYPES.map((c) => (
        <option key={c.value || 'all'} value={c.value}>
          {c.label}
        </option>
      ))}
    </select>
  )
}
