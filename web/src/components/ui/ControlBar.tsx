import { useState } from 'react';
import { RefreshCw, Square, AlertCircle } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from './Button';
import { restartAgent, stopAgent } from '../../lib/api';

interface ControlBarProps {
  agentName: string;
  onActionComplete?: () => void;
}

export function ControlBar({ agentName, onActionComplete }: ControlBarProps) {
  const [isRestarting, setIsRestarting] = useState(false);
  const [isStopping, setIsStopping] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleRestart = async () => {
    setIsRestarting(true);
    setError(null);
    try {
      await restartAgent(agentName);
      onActionComplete?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Restart failed');
    } finally {
      setIsRestarting(false);
    }
  };

  const handleStop = async () => {
    setIsStopping(true);
    setError(null);
    try {
      await stopAgent(agentName);
      onActionComplete?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Stop failed');
    } finally {
      setIsStopping(false);
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <Button
          onClick={handleRestart}
          disabled={isRestarting || isStopping}
          variant="primary"
          size="sm"
          className="flex-1"
        >
          <RefreshCw
            size={14}
            className={cn(isRestarting && 'animate-spin')}
          />
          {isRestarting ? 'Restarting...' : 'Restart'}
        </Button>
        <Button
          onClick={handleStop}
          disabled={isRestarting || isStopping}
          variant="danger"
          size="sm"
          className="flex-1"
        >
          <Square size={14} />
          {isStopping ? 'Stopping...' : 'Stop'}
        </Button>
      </div>

      {error && (
        <div className="flex items-center gap-2 p-2 bg-status-error/10 rounded text-xs text-status-error">
          <AlertCircle size={14} />
          {error}
        </div>
      )}
    </div>
  );
}
