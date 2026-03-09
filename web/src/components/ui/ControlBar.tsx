import { useState } from 'react';
import { RefreshCw, Square, AlertCircle, Zap } from 'lucide-react';
import { Button } from './Button';
import { restartAgent, stopAgent, restartMCPServer } from '../../lib/api';

interface ControlBarProps {
  name: string;
  variant: 'agent' | 'mcp-server';
  onActionComplete?: () => void;
}

export function ControlBar({ name, variant, onActionComplete }: ControlBarProps) {
  const [isRestarting, setIsRestarting] = useState(false);
  const [isStopping, setIsStopping] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleRestart = async () => {
    setIsRestarting(true);
    setError(null);
    try {
      if (variant === 'mcp-server') {
        await restartMCPServer(name);
      } else {
        await restartAgent(name);
      }
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
      await stopAgent(name);
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
        >
          {isRestarting ? (
            <RefreshCw size={14} className="animate-spin" />
          ) : (
            <Zap size={14} />
          )}
          {isRestarting ? 'Restarting...' : 'Restart'}
        </Button>
        {variant === 'agent' && (
          <Button
            onClick={handleStop}
            disabled={isRestarting || isStopping}
            variant="danger"
            size="sm"
          >
            <Square size={14} />
            {isStopping ? 'Stopping...' : 'Stop'}
          </Button>
        )}
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
