// Adapted from Tremor chartColors [v0.1.0]
// Keys stay categorical; values are semantic tokens so series follow data-theme.

export type ColorUtility = "bg" | "stroke" | "fill" | "text"

export const chartColors = {
  amber: {
    bg: "bg-primary",
    stroke: "stroke-primary",
    fill: "fill-primary",
    text: "text-primary",
  },
  teal: {
    bg: "bg-secondary",
    stroke: "stroke-secondary",
    fill: "fill-secondary",
    text: "text-secondary",
  },
  violet: {
    bg: "bg-tertiary",
    stroke: "stroke-tertiary",
    fill: "fill-tertiary",
    text: "text-tertiary",
  },
  emerald: {
    bg: "bg-status-running",
    stroke: "stroke-status-running",
    fill: "fill-status-running",
    text: "text-status-running",
  },
  blue: {
    bg: "bg-chart-blue",
    stroke: "stroke-chart-blue",
    fill: "fill-chart-blue",
    text: "text-chart-blue",
  },
  rose: {
    bg: "bg-status-error",
    stroke: "stroke-status-error",
    fill: "fill-status-error",
    text: "text-status-error",
  },
  cyan: {
    bg: "bg-chart-cyan",
    stroke: "stroke-chart-cyan",
    fill: "fill-chart-cyan",
    text: "text-chart-cyan",
  },
  gray: {
    bg: "bg-status-idle",
    stroke: "stroke-status-idle",
    fill: "fill-status-idle",
    text: "text-status-idle",
  },
} as const satisfies {
  [color: string]: {
    [key in ColorUtility]: string
  }
}

export type AvailableChartColorsKeys = keyof typeof chartColors

export const AvailableChartColors: AvailableChartColorsKeys[] = Object.keys(
  chartColors,
) as Array<AvailableChartColorsKeys>

export const constructCategoryColors = (
  categories: string[],
  colors: AvailableChartColorsKeys[],
): Map<string, AvailableChartColorsKeys> => {
  const categoryColors = new Map<string, AvailableChartColorsKeys>()
  categories.forEach((category, index) => {
    categoryColors.set(category, colors[index % colors.length])
  })
  return categoryColors
}

export const getColorClassName = (
  color: AvailableChartColorsKeys,
  type: ColorUtility,
): string => {
  const fallbackColor = {
    bg: "bg-status-idle",
    stroke: "stroke-status-idle",
    fill: "fill-status-idle",
    text: "text-status-idle",
  }
  return chartColors[color]?.[type] ?? fallbackColor[type]
}
