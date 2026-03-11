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

const defaultStrokeColors: Record<string, string> = {
  teal: "#0d9488",
  amber: "#f59e0b",
  violet: "#8b5cf6",
  emerald: "#10b981",
  blue: "#3b82f6",
  rose: "#f43f5e",
  cyan: "#06b6d4",
  gray: "#6b7280",
}

const defaultFillColors: Record<string, string> = {
  teal: "rgba(13, 148, 136, 0.15)",
  amber: "rgba(245, 158, 11, 0.15)",
  violet: "rgba(139, 92, 246, 0.15)",
  emerald: "rgba(16, 185, 129, 0.15)",
  blue: "rgba(59, 130, 246, 0.15)",
  rose: "rgba(244, 63, 94, 0.15)",
  cyan: "rgba(6, 182, 212, 0.15)",
  gray: "rgba(107, 114, 128, 0.15)",
}

const SparkChart = React.forwardRef<HTMLDivElement, SparkChartProps>(
  (
    {
      data,
      categories,
      index,
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
                  stroke={defaultStrokeColors[color] ?? defaultStrokeColors.gray}
                  fill={defaultFillColors[color] ?? defaultFillColors.gray}
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
