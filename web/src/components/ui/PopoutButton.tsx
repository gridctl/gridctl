import { AppWindow, ExternalLink } from 'lucide-react';
import { IconButton } from './IconButton';

interface PopoutButtonProps {
  onClick: () => void;
  tooltip?: string;
  disabled?: boolean;
  /** Panel is already in a detached window. Click focuses that window. */
  detached?: boolean;
  className?: string;
}

export function PopoutButton({
  onClick,
  tooltip = 'Open in separate window',
  disabled,
  detached,
  className,
}: PopoutButtonProps) {
  const label = detached ? 'Detached. Click to focus' : tooltip;
  return (
    <IconButton
      icon={detached ? AppWindow : ExternalLink}
      onClick={onClick}
      disabled={disabled}
      tooltip={label}
      size="sm"
      variant="ghost"
      className={className}
    />
  );
}
