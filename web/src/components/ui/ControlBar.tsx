import { useState } from 'react';
import { RefreshCw, Square, AlertCircle, Zap } from 'lucide-react';
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
    <div className="space-y-3">
      <div className="flex gap-2">
        <Button
          onClick={handleRestart}
          disabled={isRestarting || isStopping}
          variant="primary"
          size="sm"
          className="flex-1"
        >
          {isRestarting ? (
            <RefreshCw size={14} className="animate-spin" />
          ) : (
            <Zap size={14} />
          )}
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
        <div className="flex items-center gap-2 p-2.5 bg-status-error/10 border border-status-error/20 rounded-lg text-xs text-status-error">
          <AlertCircle size={14} className="flex-shrink-0" />
          <span>{error}</span>
        </div>
      )}
    </div>
  );
}
