import { Background, BackgroundVariant, useViewport } from '@xyflow/react';
import { useThemeColors } from '../../themes/useTheme';

/**
 * Zoom-conditional dot layer. Isolated so the stack canvas does not subscribe
 * to the viewport for a single zoom read.
 */
export function SubGrid() {
  const { zoom } = useViewport();
  const colors = useThemeColors();
  if (zoom <= 0.8) return null;
  const subGridOpacity = Math.min((zoom - 0.8) * 2.5, 1);
  return (
    <Background
      id="sub-grid"
      variant={BackgroundVariant.Dots}
      gap={20}
      size={1}
      color={`color-mix(in srgb, ${colors.secondary} ${Math.round(10 * subGridOpacity)}%, transparent)`}
    />
  );
}
