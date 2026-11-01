// Minimal sparkline chart for inline use (sidebar, cards)
// Uses Recharts AreaChart without axes, legends, or tooltips

import React from "react"
import {
  Area,
  AreaChart as RechartsAreaChart,
  ResponsiveContainer,
} from "recharts"
import { cn } from "../../lib/cn"

interface SparkChartProps extends React.HTMLAttributes<HTMLDivElement> {
  data: Record<string, number | string>[]
  categories: string[]
  index: string
  colors?: string[]
  type?: "default" | "stacked"
}

const sparkColorTokens: Record<string, string> = {
  teal: "secondary",
  amber: "primary",
  violet: "tertiary",
  emerald: "status-running",
  blue: "chart-blue",
  rose: "status-error",
  cyan: "chart-cyan",
  gray: "status-idle",
}

function sparkColor(name: string): string {
  const token = sparkColorTokens[name] ?? sparkColorTokens.gray
  return `var(--color-${token})`
}

const SparkChart = React.forwardRef<HTMLDivElement, SparkChartProps>(
  (
    {
      data,
      categories,
      index: _index,
      colors = ["teal", "amber"],
      type = "default",
      className,
      ...other
    },
    ref,
  ) => {
    const stacked = type === "stacked"

    return (
      <div ref={ref} className={cn("h-8 w-full", className)} {...other}>
        <ResponsiveContainer>
          <RechartsAreaChart
            data={data}
            margin={{ top: 1, right: 0, bottom: 0, left: 0 }}
          >
            {categories.map((category, i) => {
              const color = colors[i % colors.length]
              return (
                <Area
                  key={category}
                  type="monotone"
                  dataKey={category}
                  stroke={sparkColor(color)}
                  fill={sparkColor(color)}
                  fillOpacity={0.15}
                  strokeWidth={1.5}
                  isAnimationActive={false}
                  stackId={stacked ? "stack" : undefined}
                  dot={false}
                  activeDot={false}
                />
              )
            })}
          </RechartsAreaChart>
        </ResponsiveContainer>
      </div>
    )
  },
)

SparkChart.displayName = "SparkChart"

export { SparkChart }
