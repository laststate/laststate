import React, { useState, useRef, type ReactNode } from 'react'

interface ChartTooltipProps {
  children: ReactNode
  content: { x: string; y: number; label?: string }[]
  getValueLabel?: (value: number) => string
}

export function ChartTooltip({ children, content, getValueLabel }: ChartTooltipProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  const formatValue = (value: number) => {
    if (getValueLabel) return getValueLabel(value)
    return value.toLocaleString()
  }

  return (
    <div
      ref={containerRef}
      className="relative"
      onMouseEnter={() => setHoveredIndex(0)}
      onMouseLeave={() => setHoveredIndex(null)}
    >
      {children}
      {hoveredIndex !== null && content.length > 0 && (
        <div
          className="absolute top-0 right-0 z-50 pointer-events-none"
          style={{ transform: 'translateY(-100%)', marginTop: '-8px' }}
        >
          <div className="bg-hsl-0-0-10 border border-hsl-0-0-12 rounded-lg px-2 py-1 shadow-lg">
            <div className="text-xs font-medium text-hsl-0-0-96">
              {content[hoveredIndex]?.x || '—'}
            </div>
            <div className="text-xs text-hsl-0-0-64">
              {content.map((item, i) => (
                <div key={i} className="flex items-center gap-1">
                  <span className={`w-2 h-2 rounded-full inline-block ${
                    i === 0 ? 'bg-purple-500' : i === 1 ? 'bg-cyan-500' : 'bg-pink-500'
                  }`} />
                  <span>{item.label || 'value'}</span>
                  <span className="font-medium text-hsl-0-0-96 ml-auto">
                    {formatValue(item.y)}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
