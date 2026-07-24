package models

import (
	"errors"
	"strings"
)

// ErrInvalidQuery is returned when a sort or filter names a column that is not
// in the model's whitelist. Handlers map it to HTTP 422.
var ErrInvalidQuery = errors.New("invalid query parameter")

// QueryOpts carries list options validated at the boundary. Empty fields mean
// "not requested": empty Sort → default ordering, empty FilterField → no filter.
type QueryOpts struct {
	Sort        string // "name" (asc) or "-name" (desc); "" = default
	FilterField string // "" = no filter
	FilterValue string
}

// orderByClause returns a safe "ORDER BY <col> ASC|DESC" for a sort spec whose
// column is in allowed. A leading '-' means DESC. "" → "ORDER BY created_at
// DESC". A column not exactly present in allowed → ErrInvalidQuery.
//
// The returned column is always a member of allowed (a generated literal of the
// model's real columns), so interpolating it into SQL is safe. Values are never
// handled here — filter values are bound as ? parameters by the caller.
func orderByClause(sort string, allowed []string) (string, error) {
	if sort == "" {
		return "ORDER BY created_at DESC", nil
	}
	col := sort
	dir := "ASC"
	if strings.HasPrefix(sort, "-") {
		col = sort[1:]
		dir = "DESC"
	}
	if !contains(allowed, col) {
		return "", ErrInvalidQuery
	}
	return "ORDER BY " + col + " " + dir, nil
}

// filterField validates a filter column against allowed and returns the safe
// column name (to be interpolated), or ErrInvalidQuery. The filter value is
// bound as a ? parameter by the caller — never interpolated.
func filterField(field string, allowed []string) (string, error) {
	if !contains(allowed, field) {
		return "", ErrInvalidQuery
	}
	return field, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
