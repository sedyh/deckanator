// Package request offers small helpers around net/http that apps use
// to pull bytes or decode JSON from an endpoint in one call.
package request

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"deckanator/internal/errs"
)

// Bytes performs a GET and returns the response body on HTTP 200.
// Non-200 statuses are reported as errors.
func Bytes(url string) (_ []byte, e error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer errs.Close(&e, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// JSON performs a GET and decodes the response body into v on HTTP 200.
func JSON(url string, v any) error {
	data, err := Bytes(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
