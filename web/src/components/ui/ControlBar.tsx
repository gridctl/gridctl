import { useEffect, useRef, useState } from 'react';
import { RefreshCw, AlertCircle, Zap } from 'lucide-react';
import { Button } from './Button';
import { showToast } from './Toast';
import { restartMCPServer } from '../../lib/api';
import { requestGatewayPoll } from '../../hooks/usePolling';
import { useStackStore } from '../../stores/useStackStore';
import {
  beginRestart,
  endRestart,
  isRestartInFlight,
  useRestartInFlight,
} from '../../lib/restartsInFlight';

interface ControlBarProps {
  name: string;
}

export function ControlBar({ name }: ControlBarProps) {
  const sessions = useStackStore((s) => s.sessions);
  const isRestarting = useRestartInFlight(name);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const handleRestart = async () => {
    const target = name;
    if (isRestartInFlight(target)) return;
    setConfirming(false);
    setError(null);
    beginRestart(target);
    try {
      await restartMCPServer(target);
      showToast('success', `Restarted ${target}`);
      requestGatewayPoll();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Restart failed';
      // A control that unmounted (selection moved on) has nowhere to show
      // the inline error, so the failure goes to a toast instead.
      if (mounted.current) {
        setError(message);
      } else {
        showToast('error', `Restart of ${target} failed: ${message}`);
      }
    } finally {
      endRestart(target);
    }
  };

  const handleClick = () => {
    if (isRestarting) return;
    if (sessions > 0) {
      setError(null);
      setConfirming(true);
      return;
    }
    void handleRestart();
  };

  const sessionText =
    sessions === 1 ? '1 gateway session is active' : `${sessions} gateway sessions are active`;

  return (
    <div className="space-y-3">
      {confirming && !isRestarting ? (
        <div className="space-y-2">
          <p className="text-xs text-text-secondary">
            Restart {name}? {sessionText}; in-flight calls to {name} will fail.
          </p>
          <div className="flex gap-2">
            <Button onClick={() => void handleRestart()} variant="outline-danger" size="sm">
              Restart
            </Button>
            <Button onClick={() => setConfirming(false)} variant="ghost" size="sm">
              Cancel
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex gap-2">
          <Button onClick={handleClick} disabled={isRestarting} variant="secondary" size="sm">
            {isRestarting ? (
              <RefreshCw size={14} className="animate-spin" />
            ) : (
              <Zap size={14} />
            )}
            {isRestarting ? 'Restarting...' : 'Restart'}
          </Button>
        </div>
      )}

      {error && (
        <div
          role="alert"
          className="flex items-center gap-2 p-2.5 bg-status-error/10 border border-status-error/20 rounded-lg text-xs text-status-error"
        >
          <AlertCircle size={14} className="flex-shrink-0" />
          <span>{error}</span>
        </div>
      )}
    </div>
  );
}
