import { useCallback, useContext } from 'react';
import { UNSAFE_NavigationContext, useHref, useLocation, type Location } from 'react-router';

// Returns a builder for the next search params of a functional
// setSearchParams update. The updater's `prev` is the rendered location, which
// lags a navigation that has not rendered yet; two writers in one tick (the
// spec close and the ?node= write) would then undo each other. Declarative
// routers pass their history as the navigator, whose location moves at once on
// navigate, so it is used while it is still on the rendered path.
export function useLiveSearchParams(): (rendered: URLSearchParams) => URLSearchParams {
  const { navigator } = useContext(UNSAFE_NavigationContext);
  const renderedPath = useHref({ pathname: useLocation().pathname });
  return useCallback(
    (rendered: URLSearchParams) => {
      const live = (navigator as { location?: Location }).location;
      return new URLSearchParams(live && live.pathname === renderedPath ? live.search : rendered);
    },
    [navigator, renderedPath],
  );
}
