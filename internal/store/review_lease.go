package store

import "errors"

// ErrReviewLeaseBusy reports that another worker currently owns the namespace.
var ErrReviewLeaseBusy = errors.New("review lease is already held")
