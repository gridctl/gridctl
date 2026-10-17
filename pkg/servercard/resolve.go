package servercard

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Resolve fetches <url>/server-card and, on 404, the origin catalog.
// Catalog entries on another host are skipped; they are not a selected
// remote. A selected remote on a different scheme, host, or port is
// refused. URL templates are refused. The returned card is the one whose
// remote was accepted.
func Resolve(ctx context.Context, submittedURL string, opts FetchOptions) (*ServerCard, error) {
	cardURL, err := CardURLFor(submittedURL)
	if err != nil {
		return nil, err
	}
	card, err := Fetch(ctx, cardURL, opts)
	if err == nil {
		if _, selErr := selectRemote(card, submittedURL); selErr != nil {
			return nil, selErr
		}
		return card, nil
	}
	if !errors.Is(err, ErrNoCard) {
		if errors.Is(err, ErrInvalidCard) {
			return nil, err
		}
		// A present-but-unreadable canonical card is a fetch failure.
		// Fall through to the catalog only for a missing card.
		if !errors.Is(err, ErrNoCard) {
			return nil, err
		}
	}
	return resolveFromCatalog(ctx, submittedURL, cardURL, opts)
}

func resolveFromCatalog(ctx context.Context, submittedURL, wantCard string, opts FetchOptions) (*ServerCard, error) {
	catalogURL, err := CatalogURLFor(submittedURL)
	if err != nil {
		return nil, err
	}
	cat, err := FetchCatalog(ctx, catalogURL, opts)
	if err != nil {
		if errors.Is(err, ErrNoCard) {
			return nil, fmt.Errorf("%w: %s", ErrNoCard, submittedURL)
		}
		return nil, err
	}
	var last error
	for _, entry := range cat.Entries {
		if entry.URL == "" || strings.Contains(entry.URL, "{") {
			continue
		}
		// A catalog often lists cards on other hosts. Those entries are not
		// the selected remote, so skip them and keep looking for a match.
		// ErrOffOrigin is reserved for a same-origin card whose remote is
		// off-origin, which selectRemote reports below.
		if !SameOrigin(entry.URL, submittedURL) {
			continue
		}
		card, ferr := Fetch(ctx, entry.URL, opts)
		if ferr != nil {
			last = ferr
			continue
		}
		remote, selErr := selectRemote(card, submittedURL)
		if selErr != nil {
			if errors.Is(selErr, ErrOffOrigin) || errors.Is(selErr, ErrTemplateURL) {
				return nil, selErr
			}
			last = selErr
			continue
		}
		if entry.URL == wantCard || remote.URL == submittedURL || strings.TrimSuffix(remote.URL, "/") == strings.TrimSuffix(submittedURL, "/") {
			return card, nil
		}
		last = fmt.Errorf("%w: catalog entry did not match %s", ErrNoCard, submittedURL)
	}
	if last != nil && !errors.Is(last, ErrNoCard) {
		return nil, last
	}
	return nil, fmt.Errorf("%w: %s", ErrNoCard, submittedURL)
}
