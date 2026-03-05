import { ExternalLink } from 'lucide-react';
import { IconButton } from './IconButton';

interface PopoutButtonProps {
  onClick: () => void;
  tooltip?: string;
  disabled?: boolean;
  className?: string;
}

export function PopoutButton({ onClick, tooltip = 'Open in new tab', disabled, className }: PopoutButtonProps) {
  return (
    <IconButton
      icon={ExternalLink}
      onClick={onClick}
      disabled={disabled}
      tooltip={tooltip}
      size="sm"
      variant="ghost"
      className={className}
    />
  );
}
