import { FormInput, Code2 } from 'lucide-react';
import { SegmentedControl } from './SegmentedControl';

interface ExpertModeToggleProps {
  expertMode: boolean;
  onToggle: (enabled: boolean) => void;
}

export function ExpertModeToggle({ expertMode, onToggle }: ExpertModeToggleProps) {
  return (
    <SegmentedControl
      ariaLabel="Editor mode"
      value={expertMode ? 'yaml' : 'form'}
      onChange={(mode) => onToggle(mode === 'yaml')}
      options={[
        { value: 'form', label: 'Form', icon: FormInput },
        { value: 'yaml', label: 'YAML', icon: Code2 },
      ]}
    />
  );
}
