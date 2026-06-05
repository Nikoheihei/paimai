/**
 * PriceDisplay — 价格展示组件
 * 将后端返回的"分"转换为"元"并美化展示
 */

type Props = {
  /** 金额，单位：分 */
  cents: number
  /** 显示尺寸 */
  size?: 'sm' | 'md' | 'lg' | 'xl'
  /** 是否显示成交绿色（sold 状态时用） */
  sold?: boolean
  /** 前缀文字（如"当前价"/"成交价"/"起拍价"） */
  label?: string
  /** 自定义 CSS 类名 */
  className?: string
}

const SIZE_CLASS: Record<string, string> = {
  sm: 'price-sm',
  md: 'price-md',
  lg: 'price-lg',
  xl: 'price-xl',
}

export default function PriceDisplay({ cents, size = 'lg', sold = false, label, className = '' }: Props) {
  const yuan = (cents / 100).toFixed(2)
  // 大额简化显示（>= 10000 元）
  const display = cents >= 1_000_000
    ? `${(cents / 100 / 10000).toFixed(1)}万`
    : yuan

  return (
    <div className={`price-display ${className}`}>
      {label && <span className="price-label">{label}</span>}
      <span className={`price-value ${SIZE_CLASS[size]} ${sold ? 'price-sold' : ''}`}>
        &yen;{display}
      </span>
    </div>
  )
}
